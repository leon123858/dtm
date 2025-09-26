package rabbit

import (
	"context"
	"dtm/mq/mq"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// UnmarshalFunc is a generic function type for unmarshaling bytes into a message of type M.
type UnmarshalFunc[M any] func(data []byte) (M, error)

// consumerInfo holds details about an active consumer.
type consumerInfo struct {
	tag     string
	channel *amqp.Channel
	cancel  chan struct{}
}

// GenericRabbitMQService provides a generic implementation for message queue operations.
type GenericRabbitMQService[M any] struct {
	conn            *amqp.Connection
	publishChannel  *amqp.Channel
	publishMutex    sync.Mutex
	exchangeName    string
	activeConsumers map[uuid.UUID]*consumerInfo
	consumersMutex  sync.Mutex
}

func NewGenericRabbitMQService[M any](conn *amqp.Connection, exchangeName string) (*GenericRabbitMQService[M], error) {
	if conn == nil {
		return nil, fmt.Errorf("RabbitMQ connection is nil")
	}
	pubCh, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open publish channel: %w", err)
	}
	err = pubCh.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil)
	if err != nil {
		_ = pubCh.Close()
		return nil, fmt.Errorf("failed to declare exchange %s: %w", exchangeName, err)
	}
	return &GenericRabbitMQService[M]{
		conn: conn, publishChannel: pubCh, exchangeName: exchangeName, activeConsumers: make(map[uuid.UUID]*consumerInfo),
	}, nil
}

func (s *GenericRabbitMQService[M]) Publish(msg mq.TopicProvider) error {
	s.publishMutex.Lock()
	defer s.publishMutex.Unlock()
	typeName := reflect.TypeOf(msg).Name()
	if s.publishChannel == nil || s.publishChannel.IsClosed() {
		return fmt.Errorf("publish channel for %s is not available", typeName)
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", typeName, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	routingKey := msg.GetTopic().String()
	return s.publishChannel.PublishWithContext(ctx, s.exchangeName, routingKey, false, false,
		amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body})
}

func (s *GenericRabbitMQService[M]) Subscribe(tripId uuid.UUID, unmarshalFn UnmarshalFunc[M]) (uuid.UUID, <-chan M, error) {
	subscriptionID := uuid.New()
	typeName := reflect.TypeOf(*new(M)).Name()
	if s.publishChannel == nil || s.publishChannel.IsClosed() {
		return uuid.Nil, nil, fmt.Errorf("publish channel for %s is not available", typeName)
	}
	subChannel, err := s.conn.Channel()
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("failed to open channel for %s subscription: %w", typeName, err)
	}
	queue, err := subChannel.QueueDeclare("", true, true, true, false, nil)
	if err != nil {
		_ = subChannel.Close()
		return uuid.Nil, nil, fmt.Errorf("failed to declare queue for %s: %w", typeName, err)
	}
	if err = subChannel.QueueBind(queue.Name, tripId.String(), s.exchangeName, false, nil); err != nil {
		_ = subChannel.Close()
		return uuid.Nil, nil, fmt.Errorf("failed to bind queue for %s: %w", typeName, err)
	}
	if err = subChannel.Qos(1, 0, false); err != nil {
		_ = subChannel.Close()
		return uuid.Nil, nil, fmt.Errorf("failed to set QoS for %s: %w", typeName, err)
	}
	consumerTag := fmt.Sprintf("%s-consumer-%s", typeName, subscriptionID.String())
	deliveries, err := subChannel.Consume(queue.Name, consumerTag, false, true, false, false, nil)
	if err != nil {
		_ = subChannel.Close()
		return uuid.Nil, nil, fmt.Errorf("failed to register consumer for %s: %w", typeName, err)
	}
	msgChan := make(chan M, 5)
	stopChan := make(chan struct{})
	s.consumersMutex.Lock()
	cusInfo := consumerInfo{
		tag:     consumerTag,
		channel: subChannel,
		cancel:  stopChan,
	}
	s.activeConsumers[subscriptionID] = &cusInfo
	s.consumersMutex.Unlock()
	go func() {
		defer func() {
			s.consumersMutex.Lock()
			delete(s.activeConsumers, subscriptionID)
			s.consumersMutex.Unlock()
			err = subChannel.Cancel(consumerTag, false)
			if err != nil {
				log.Printf("Error canceling consumer %s for %s: %v", consumerTag, typeName, err)
			}
			err = subChannel.Close()
			if err != nil {
				log.Printf("Error closing channel for consumer %s: %v", consumerTag, err)
			}
			close(msgChan)
			// log.Printf("Consumer goroutine for %s subscription %s shut down.", typeName, subscriptionID)
		}()
		for {
			select {
			case <-stopChan:
				// log.Printf("Stopping %s consumer for subscription %s.", typeName, subscriptionID)
				return
			case delivery, ok := <-deliveries:
				if !ok {
					// log.Printf("Delivery channel closed for %s subscription %s.", typeName, subscriptionID)
					return
				}
				msg, err := unmarshalFn(delivery.Body)
				if err != nil {
					log.Printf("Error unmarshaling %s for %s: %v. Body: %s", typeName, subscriptionID, err, string(delivery.Body))
					_ = delivery.Nack(false, false)
					continue
				}
				select {
				case msgChan <- msg:
				case <-stopChan:
					// log.Printf("%s consumer %s stopping while sending to msgChan.", typeName, subscriptionID)
					_ = delivery.Ack(false)
					return
				case <-time.After(2 * time.Second):
					log.Printf("Timeout sending %s to msgChan for %s.", typeName, subscriptionID)
					_ = delivery.Ack(false)
					continue
				}
				_ = delivery.Ack(false)
			}
		}
	}()
	// log.Printf("Subscribed to %s for tripId %s. Subscription ID: %s", typeName, tripId, subscriptionID)
	return subscriptionID, msgChan, nil
}

func (s *GenericRabbitMQService[M]) DeSubscribe(id uuid.UUID) error {
	s.consumersMutex.Lock()
	info, ok := s.activeConsumers[id]
	if ok {
		delete(s.activeConsumers, id)
	}
	s.consumersMutex.Unlock()
	if !ok {
		return fmt.Errorf("subscription ID %s not found for %s service", id, reflect.TypeOf(*new(M)).Name())
	}
	// log.Printf("DeSubscribing %s subscription ID %s, tag %s", reflect.TypeOf(*new(M)).Name(), id, info.tag)
	select {
	case <-info.cancel:
	default:
		close(info.cancel)
	}
	return nil
}

func (s *GenericRabbitMQService[M]) Close() error {
	s.publishMutex.Lock()
	defer s.publishMutex.Unlock()
	if s.publishChannel != nil {
		if err := s.publishChannel.Close(); err != nil {
			return fmt.Errorf("failed to close publish channel: %w", err)
		}
		s.publishChannel = nil
	}
	s.consumersMutex.Lock()
	defer s.consumersMutex.Unlock()
	for id, info := range s.activeConsumers {
		// log.Printf("Closing consumer %s with ID %s", info.tag, id)
		close(info.cancel)
		if err := info.channel.Close(); err != nil {
			log.Printf("Error closing channel for consumer %s: %v", info.tag, err)
		}
		delete(s.activeConsumers, id)
	}
	return nil
}

type TripRecordMQ struct {
	genericService   *GenericRabbitMQService[mq.TripRecordMessage]
	configuredAction mq.Action
}

func NewTripRecordMessageQueue(conn *amqp.Connection, exchangeName string, action mq.Action) (*TripRecordMQ, error) {
	gs, err := NewGenericRabbitMQService[mq.TripRecordMessage](conn, exchangeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic service for TripRecord: %w", err)
	}
	return &TripRecordMQ{genericService: gs, configuredAction: action}, nil
}
func (q *TripRecordMQ) GetAction() mq.Action                   { return q.configuredAction }
func (q *TripRecordMQ) Publish(msg mq.TripRecordMessage) error { return q.genericService.Publish(msg) }
func unmarshalTripRecordMessage(data []byte) (mq.TripRecordMessage, error) {
	var msg mq.TripRecordMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
func (q *TripRecordMQ) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	return q.genericService.Subscribe(tripId, unmarshalTripRecordMessage)
}
func (q *TripRecordMQ) DeSubscribe(id uuid.UUID) error { return q.genericService.DeSubscribe(id) }

type TripAddressMQ struct {
	genericService   *GenericRabbitMQService[mq.TripAddressMessage]
	configuredAction mq.Action
}

func NewTripAddressMessageQueue(conn *amqp.Connection, exchangeName string, action mq.Action) (*TripAddressMQ, error) {
	gs, err := NewGenericRabbitMQService[mq.TripAddressMessage](conn, exchangeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic service for TripAddress: %w", err)
	}
	return &TripAddressMQ{genericService: gs, configuredAction: action}, nil
}
func (q *TripAddressMQ) GetAction() mq.Action { return q.configuredAction }
func (q *TripAddressMQ) Publish(msg mq.TripAddressMessage) error {
	return q.genericService.Publish(msg)
}
func unmarshalTripAddressMessage(data []byte) (mq.TripAddressMessage, error) {
	var msg mq.TripAddressMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
func (q *TripAddressMQ) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	return q.genericService.Subscribe(tripId, unmarshalTripAddressMessage)
}
func (q *TripAddressMQ) DeSubscribe(id uuid.UUID) error { return q.genericService.DeSubscribe(id) }

// --------- trip message queue wrapper implementation ---------

type TripMessageQueueWrapper struct {
	RecordMQArray  [mq.ActionCnt]*TripRecordMQ
	AddressMQArray [mq.ActionCnt]*TripAddressMQ
}

func (wrapper *TripMessageQueueWrapper) GetTripRecordMessageQueue(action mq.Action) mq.TripRecordMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle the error as needed
	}
	return wrapper.RecordMQArray[action]
}

func (wrapper *TripMessageQueueWrapper) GetTripAddressMessageQueue(action mq.Action) mq.TripAddressMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle the error as needed
	}
	if wrapper.AddressMQArray[action] == nil {
		return nil
	}
	return wrapper.AddressMQArray[action]
}

// NewRabbitTripMessageQueueWrapper creates a new instance of RabbitTripMessageQueueWrapper.
func NewRabbitTripMessageQueueWrapper(conn *amqp.Connection) (mq.TripMessageQueueWrapper, error) {
	wrapper := TripMessageQueueWrapper{}
	var err error
	// address need add and remove
	wrapper.AddressMQArray[mq.ActionCreate], err = NewTripAddressMessageQueue(conn, fmt.Sprintf("trip_address_exchange_%d", mq.ActionCreate), mq.ActionCreate)
	if err != nil {
		return nil, fmt.Errorf("error creating TripAddressMessageQueue for ActionCreate: %w", err)
	}
	wrapper.AddressMQArray[mq.ActionUpdate] = nil
	wrapper.AddressMQArray[mq.ActionDelete], err = NewTripAddressMessageQueue(conn, fmt.Sprintf("trip_address_exchange_%d", mq.ActionDelete), mq.ActionDelete)
	if err != nil {
		return nil, fmt.Errorf("error creating TripAddressMessageQueue for ActionDelete: %w", err)
	}
	// record need add, update and delete
	wrapper.RecordMQArray[mq.ActionCreate], err = NewTripRecordMessageQueue(conn, fmt.Sprintf("trip_record_exchange_%d", mq.ActionCreate), mq.ActionCreate)
	if err != nil {
		return nil, fmt.Errorf("error creating TripRecordMessageQueue for ActionCreate: %w", err)
	}
	wrapper.RecordMQArray[mq.ActionUpdate], err = NewTripRecordMessageQueue(conn, fmt.Sprintf("trip_record_exchange_%d", mq.ActionUpdate), mq.ActionUpdate)
	if err != nil {
		return nil, fmt.Errorf("error creating TripRecordMessageQueue for ActionUpdate: %w", err)
	}
	wrapper.RecordMQArray[mq.ActionDelete], err = NewTripRecordMessageQueue(conn, fmt.Sprintf("trip_record_exchange_%d", mq.ActionDelete), mq.ActionDelete)
	if err != nil {
		return nil, fmt.Errorf("error creating TripRecordMessageQueue for ActionDelete: %w", err)
	}

	return &wrapper, nil
}

// ------- implement utils function --------------

//func RecordBytesToTripRecordMessage(data []byte) (mq.TripRecordMessage, error) {
//	var msg mq.TripRecordMessage
//	err := json.Unmarshal(data, &msg)
//	if err != nil {
//		return mq.TripRecordMessage{}, fmt.Errorf("failed to unmarshal TripRecordMessage: %w", err)
//	}
//	return msg, nil
//}
//
//func AddressBytesToTripAddressMessage(data []byte) (mq.TripAddressMessage, error) {
//	var msg mq.TripAddressMessage
//	err := json.Unmarshal(data, &msg)
//	if err != nil {
//		return mq.TripAddressMessage{}, fmt.Errorf("failed to unmarshal TripAddressMessage: %w", err)
//	}
//	return msg, nil
//}
