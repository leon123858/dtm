package rabbit

import (
	"context" // Adjust import path if necessary
	"dtm/mq/mq"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
)

const (
	exchangeName = "trip_events_exchange" // All trip-related events go through this exchange
)

// Define routing keys for different actions and message types
const (
	recordCreateRoutingKey  = "record.create"
	recordUpdateRoutingKey  = "record.update"
	recordDeleteRoutingKey  = "record.delete"
	addressCreateRoutingKey = "address.create"
	addressDeleteRoutingKey = "address.delete"
)

// Helper to get routing key based on action and message type
func getRoutingKey(action mq.Action, msgType string) string {
	switch msgType {
	case "record":
		switch action {
		case mq.ActionCreate:
			return recordCreateRoutingKey
		case mq.ActionUpdate:
			return recordUpdateRoutingKey
		case mq.ActionDelete:
			return recordDeleteRoutingKey
		}
	case "address":
		switch action {
		case mq.ActionCreate:
			return addressCreateRoutingKey
		case mq.ActionDelete:
			return addressDeleteRoutingKey
		}
	}
	return "" // Should not happen with valid inputs
}

// rabbitTripRecordMessageQueue implements mq.TripRecordMessageQueue for RabbitMQ
type rabbitTripRecordMessageQueue struct {
	action     mq.Action
	conn       *amqp091.Connection
	channel    *amqp091.Channel
	queueName  string
	routingKey string
	mu         sync.RWMutex // Protects the consumers map
	consumers  map[uuid.UUID]chan mq.TripRecordMessage
}

// NewRabbitTripRecordMessageQueue creates a new RabbitMQ message queue for TripRecordMessages.
func NewRabbitTripRecordMessageQueue(action mq.Action, conn *amqp091.Connection) (mq.TripRecordMessageQueue, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	queueName := fmt.Sprintf("trip_record_%d_queue", action)
	routingKey := getRoutingKey(action, "record")

	err = DeclareQueueAndExchange(ch, queueName, exchangeName, routingKey)
	if err != nil {
		ch.Close()
		return nil, err
	}

	return &rabbitTripRecordMessageQueue{
		action:     action,
		conn:       conn,
		channel:    ch,
		queueName:  queueName,
		routingKey: routingKey,
		consumers:  make(map[uuid.UUID]chan mq.TripRecordMessage),
	}, nil
}

// GetAction returns the action associated with this queue.
func (q *rabbitTripRecordMessageQueue) GetAction() mq.Action {
	return q.action
}

// Publish sends a TripRecordMessage to the RabbitMQ queue.
func (q *rabbitTripRecordMessageQueue) Publish(msg mq.TripRecordMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = q.channel.PublishWithContext(ctx,
		exchangeName, // exchange
		q.routingKey, // routing key
		false,        // mandatory
		false,        // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        body,
		})
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

// Subscribe returns a read-only channel for TripRecordMessages.
func (q *rabbitTripRecordMessageQueue) Subscribe() (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	msgs, err := q.channel.Consume(
		q.queueName, // queue
		"",          // consumer
		true,        // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("failed to register a consumer: %w", err)
	}

	subscriberID := uuid.New()
	outputChan := make(chan mq.TripRecordMessage)

	q.mu.Lock()
	q.consumers[subscriberID] = outputChan
	q.mu.Unlock()

	go func() {
		defer func() {
			q.mu.Lock()
			// Clean up the consumer channel upon goroutine exit
			if ch, ok := q.consumers[subscriberID]; ok {
				close(ch)
				delete(q.consumers, subscriberID)
			}
			q.mu.Unlock()
			// log.Printf("RabbitMQ record consumer %s for queue %s stopped.", subscriberID, q.queueName)
		}()

		for d := range msgs {
			var msg mq.TripRecordMessage
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				log.Printf("Failed to unmarshal TripRecordMessage: %v", err)
				continue
			}

			q.mu.RLock()
			if ch, ok := q.consumers[subscriberID]; ok {
				select {
				case ch <- msg:
					// Message sent to consumer
				case <-time.After(1 * time.Second): // Prevent blocking indefinitely
					log.Printf("Timeout sending message to TripRecordMessage consumer %s. Skipping.", subscriberID)
				}
			} else {
				// Consumer was unsubscribed while message was in flight
				log.Printf("TripRecordMessage consumer %s no longer active. Skipping message.", subscriberID)
				return // Exit goroutine if consumer is gone
			}
			q.mu.RUnlock()
		}
		// log.Printf("RabbitMQ record delivery channel for %s closed.", subscriberID)
	}()

	return subscriberID, outputChan, nil
}

// DeSubscribe removes a subscriber by its ID.
func (q *rabbitTripRecordMessageQueue) DeSubscribe(subscriberID uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ch, ok := q.consumers[subscriberID]; ok {
		delete(q.consumers, subscriberID)
		close(ch) // Close the channel to signal the consumer goroutine to stop
		return nil
	}
	return fmt.Errorf("consumer with ID %s not found for queue %s", subscriberID, q.queueName)
}

// rabbitTripAddressMessageQueue implements mq.TripAddressMessageQueue for RabbitMQ
type rabbitTripAddressMessageQueue struct {
	action     mq.Action
	conn       *amqp091.Connection
	channel    *amqp091.Channel
	queueName  string
	routingKey string
	mu         sync.RWMutex // Protects the consumers map
	consumers  map[uuid.UUID]chan mq.TripAddressMessage
}

// NewRabbitTripAddressMessageQueue creates a new RabbitMQ message queue for TripAddressMessages.
func NewRabbitTripAddressMessageQueue(action mq.Action, conn *amqp091.Connection) (mq.TripAddressMessageQueue, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	queueName := fmt.Sprintf("trip_address_%d_queue", action)
	routingKey := getRoutingKey(action, "address")

	err = DeclareQueueAndExchange(ch, queueName, exchangeName, routingKey)
	if err != nil {
		ch.Close()
		return nil, err
	}

	return &rabbitTripAddressMessageQueue{
		action:     action,
		conn:       conn,
		channel:    ch,
		queueName:  queueName,
		routingKey: routingKey,
		consumers:  make(map[uuid.UUID]chan mq.TripAddressMessage),
	}, nil
}

// GetAction returns the action associated with this queue.
func (q *rabbitTripAddressMessageQueue) GetAction() mq.Action {
	return q.action
}

// Publish sends a TripAddressMessage to the RabbitMQ queue.
func (q *rabbitTripAddressMessageQueue) Publish(msg mq.TripAddressMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = q.channel.PublishWithContext(ctx,
		exchangeName, // exchange
		q.routingKey, // routing key
		false,        // mandatory
		false,        // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        body,
		})
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

// Subscribe returns a read-only channel for TripAddressMessages.
func (q *rabbitTripAddressMessageQueue) Subscribe() (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	msgs, err := q.channel.Consume(
		q.queueName, // queue
		"",          // consumer
		true,        // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("failed to register a consumer: %w", err)
	}

	subscriberID := uuid.New()
	outputChan := make(chan mq.TripAddressMessage)

	q.mu.Lock()
	q.consumers[subscriberID] = outputChan
	q.mu.Unlock()

	go func() {
		defer func() {
			q.mu.Lock()
			if ch, ok := q.consumers[subscriberID]; ok {
				close(ch)
				delete(q.consumers, subscriberID)
			}
			q.mu.Unlock()
			// log.Printf("RabbitMQ address consumer %s for queue %s stopped.", subscriberID, q.queueName)
		}()

		for d := range msgs {
			var msg mq.TripAddressMessage
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				log.Printf("Failed to unmarshal TripAddressMessage: %v", err)
				continue
			}

			q.mu.RLock()
			if ch, ok := q.consumers[subscriberID]; ok {
				select {
				case ch <- msg:
					// Message sent to consumer
				case <-time.After(1 * time.Second): // Prevent blocking indefinitely
					log.Printf("Timeout sending message to TripAddressMessage consumer %s. Skipping.", subscriberID)
				}
			} else {
				log.Printf("TripAddressMessage consumer %s no longer active. Skipping message.", subscriberID)
				return
			}
			q.mu.RUnlock()
		}
		// log.Printf("RabbitMQ address delivery channel for %s closed.", subscriberID)
	}()

	return subscriberID, outputChan, nil
}

// DeSubscribe removes a subscriber by its ID.
func (q *rabbitTripAddressMessageQueue) DeSubscribe(subscriberID uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ch, ok := q.consumers[subscriberID]; ok {
		delete(q.consumers, subscriberID)
		close(ch) // Close the channel to signal the consumer goroutine to stop
		return nil
	}
	return fmt.Errorf("consumer with ID %s not found for queue %s", subscriberID, q.queueName)
}

// rabbitTripMessageQueueWrapper implements mq.TripMessageQueueWrapper for RabbitMQ
type rabbitTripMessageQueueWrapper struct {
	RecordMQArray  [mq.ActionCnt]mq.TripRecordMessageQueue
	AddressMQArray [mq.ActionCnt]mq.TripAddressMessageQueue
	conn           *amqp091.Connection // Keep a reference to the connection to close it later
}

// NewRabbitTripMessageQueueWrapper creates a new instance of rabbitTripMessageQueueWrapper.
func NewRabbitTripMessageQueueWrapper(conn *amqp091.Connection) (mq.TripMessageQueueWrapper, error) {
	wrapper := &rabbitTripMessageQueueWrapper{
		conn: conn,
	}

	var err error

	// Initialize Record MQs
	wrapper.RecordMQArray[mq.ActionCreate], err = NewRabbitTripRecordMessageQueue(mq.ActionCreate, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create record create mq: %w", err)
	}
	wrapper.RecordMQArray[mq.ActionUpdate], err = NewRabbitTripRecordMessageQueue(mq.ActionUpdate, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create record update mq: %w", err)
	}
	wrapper.RecordMQArray[mq.ActionDelete], err = NewRabbitTripRecordMessageQueue(mq.ActionDelete, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create record delete mq: %w", err)
	}

	// Initialize Address MQs
	wrapper.AddressMQArray[mq.ActionCreate], err = NewRabbitTripAddressMessageQueue(mq.ActionCreate, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create address create mq: %w", err)
	}
	wrapper.AddressMQArray[mq.ActionDelete], err = NewRabbitTripAddressMessageQueue(mq.ActionDelete, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create address delete mq: %w", err)
	}

	return wrapper, nil
}

// GetTripRecordMessageQueue returns the appropriate TripRecordMessageQueue based on the action.
func (wrapper *rabbitTripMessageQueueWrapper) GetTripRecordMessageQueue(action mq.Action) mq.TripRecordMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle out-of-bounds error
	}
	return wrapper.RecordMQArray[action]
}

// GetTripAddressMessageQueue returns the appropriate TripAddressMessageQueue based on the action.
func (wrapper *rabbitTripMessageQueueWrapper) GetTripAddressMessageQueue(action mq.Action) mq.TripAddressMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle out-of-bounds error
	}
	return wrapper.AddressMQArray[action]
}

// Close closes all channels and the RabbitMQ connection.
func (wrapper *rabbitTripMessageQueueWrapper) Close() {
	for _, q := range wrapper.RecordMQArray {
		if rmq, ok := q.(*rabbitTripRecordMessageQueue); ok && rmq.channel != nil {
			rmq.channel.Close()
		}
	}
	for _, q := range wrapper.AddressMQArray {
		if rmq, ok := q.(*rabbitTripAddressMessageQueue); ok && rmq.channel != nil {
			rmq.channel.Close()
		}
	}
	if wrapper.conn != nil {
		wrapper.conn.Close()
	}
}
