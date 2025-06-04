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
	err = pubCh.ExchangeDeclare(exchangeName, "topic", false, true, false, false, nil)
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
		log.Printf("Publish channel for %s (type %s) is closed. Reopening.", s.exchangeName, typeName)
		ch, err := s.conn.Channel()
		if err != nil {
			return fmt.Errorf("publish channel closed, failed to reopen for %s: %w", typeName, err)
		}
		err = ch.ExchangeDeclare(s.exchangeName, "topic", true, false, false, false, nil)
		if err != nil {
			_ = ch.Close()
			return fmt.Errorf("failed to redeclare exchange %s on reopened channel for %s: %w", s.exchangeName, typeName, err)
		}
		s.publishChannel = ch
		log.Printf("Publish channel for %s (type %s) reopened.", s.exchangeName, typeName)
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
	subChannel, err := s.conn.Channel()
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("failed to open channel for %s subscription: %w", typeName, err)
	}
	queue, err := subChannel.QueueDeclare("", false, true, true, false, nil)
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
	deliveries, err := subChannel.Consume(queue.Name, consumerTag, false, false, false, false, nil)
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
			_ = subChannel.Cancel(consumerTag, false)
			_ = subChannel.Close()
			close(msgChan)
			log.Printf("Consumer goroutine for %s subscription %s shut down.", typeName, subscriptionID)
		}()
		for {
			select {
			case <-stopChan:
				log.Printf("Stopping %s consumer for subscription %s.", typeName, subscriptionID)
				return
			case delivery, ok := <-deliveries:
				if !ok {
					log.Printf("Delivery channel closed for %s subscription %s.", typeName, subscriptionID)
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
					log.Printf("%s consumer %s stopping while sending to msgChan.", typeName, subscriptionID)
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
	log.Printf("Subscribed to %s for tripId %s. Subscription ID: %s", typeName, tripId, subscriptionID)
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
	log.Printf("DeSubscribing %s subscription ID %s, tag %s", reflect.TypeOf(*new(M)).Name(), id, info.tag)
	select {
	case <-info.cancel:
	default:
		close(info.cancel)
	}
	return nil
}

// --- tripRecordMQ implementation ---
type tripRecordMQ struct {
	genericService   *GenericRabbitMQService[mq.TripRecordMessage]
	configuredAction mq.Action
}

func NewTripRecordMessageQueue(conn *amqp.Connection, exchangeName string, action mq.Action) (mq.TripRecordMessageQueue, error) {
	gs, err := NewGenericRabbitMQService[mq.TripRecordMessage](conn, exchangeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic service for TripRecord: %w", err)
	}
	return &tripRecordMQ{genericService: gs, configuredAction: action}, nil
}
func (q *tripRecordMQ) GetAction() mq.Action                   { return q.configuredAction }
func (q *tripRecordMQ) Publish(msg mq.TripRecordMessage) error { return q.genericService.Publish(msg) }
func unmarshalTripRecordMessage(data []byte) (mq.TripRecordMessage, error) {
	var msg mq.TripRecordMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
func (q *tripRecordMQ) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	return q.genericService.Subscribe(tripId, unmarshalTripRecordMessage)
}
func (q *tripRecordMQ) DeSubscribe(id uuid.UUID) error { return q.genericService.DeSubscribe(id) }

// --- tripAddressMQ implementation ---
type tripAddressMQ struct {
	genericService   *GenericRabbitMQService[mq.TripAddressMessage]
	configuredAction mq.Action
}

func NewTripAddressMessageQueue(conn *amqp.Connection, exchangeName string, action mq.Action) (mq.TripAddressMessageQueue, error) {
	gs, err := NewGenericRabbitMQService[mq.TripAddressMessage](conn, exchangeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic service for TripAddress: %w", err)
	}
	return &tripAddressMQ{genericService: gs, configuredAction: action}, nil
}
func (q *tripAddressMQ) GetAction() mq.Action { return q.configuredAction }
func (q *tripAddressMQ) Publish(msg mq.TripAddressMessage) error {
	return q.genericService.Publish(msg)
}
func unmarshalTripAddressMessage(data []byte) (mq.TripAddressMessage, error) {
	var msg mq.TripAddressMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
func (q *tripAddressMQ) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	return q.genericService.Subscribe(tripId, unmarshalTripAddressMessage)
}
func (q *tripAddressMQ) DeSubscribe(id uuid.UUID) error { return q.genericService.DeSubscribe(id) }
