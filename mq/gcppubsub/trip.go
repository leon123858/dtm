package gcppubsub

import (
	"context"
	"dtm/mq/mq"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/google/uuid"
)

const (
	tripIDAttribute = "tripId"
)

// subscriptionInfo holds details about an active Pub/Sub subscription.
type subscriptionInfo struct {
	gcpSubscription *pubsub.Subscription
	cancel          context.CancelFunc
}

// GenericPubSubService provides a generic implementation for GCP Pub/Sub operations.
type GenericPubSubService[M any] struct {
	client              *pubsub.Client
	topic               *pubsub.Topic
	activeSubscriptions map[uuid.UUID]*subscriptionInfo
	subscriptionsMutex  sync.Mutex
	ctx                 context.Context
}

// NewGenericPubSubService creates and initializes a generic service for a specific message type.
// It ensures the underlying Pub/Sub topic exists, creating it if necessary.
func NewGenericPubSubService[M any](ctx context.Context, client *pubsub.Client, topicID string) (*GenericPubSubService[M], error) {
	if client == nil {
		return nil, fmt.Errorf("GCP Pub/Sub client is nil")
	}

	topic := client.Topic(topicID)
	exists, err := topic.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existence of topic %s: %w", topicID, err)
	}
	if !exists {
		topic, err = client.CreateTopic(ctx, topicID)
		if err != nil {
			return nil, fmt.Errorf("failed to create topic %s: %w", topicID, err)
		}
		log.Printf("Created Pub/Sub topic: %s", topicID)
	}

	return &GenericPubSubService[M]{
		client:              client,
		topic:               topic,
		activeSubscriptions: make(map[uuid.UUID]*subscriptionInfo),
		ctx:                 ctx,
	}, nil
}

// Publish sends a message to the configured Pub/Sub topic with the tripId as an attribute.
func (s *GenericPubSubService[M]) Publish(msg mq.TopicProvider) error {
	typeName := reflect.TypeOf(msg).Name()
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", typeName, err)
	}

	routingKey := msg.GetTopic().String()
	pubsubMsg := &pubsub.Message{
		Data: body,
		Attributes: map[string]string{
			tripIDAttribute: routingKey,
		},
	}

	// Publish is non-blocking. The client library handles batching and sending.
	result := s.topic.Publish(s.ctx, pubsubMsg)
	// We can optionally wait for the result to confirm, but for high throughput,
	// we can proceed without waiting. The library will handle retries.
	_, err = result.Get(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to publish %s to topic %s: %w", typeName, s.topic.ID(), err)
	}
	// _ = result // Avoid unused variable error if not waiting on Get()
	return nil
}

// Subscribe creates a new filtered subscription on GCP and starts listening for messages.
func (s *GenericPubSubService[M]) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan M, error) {
	subscriptionID := uuid.New() // Internal ID for tracking
	typeName := reflect.TypeOf(*new(M)).Name()

	// Create a unique, descriptive subscription name for GCP.
	gcpSubName := fmt.Sprintf("sub-%s-%s-%s", typeName, tripId.String(), subscriptionID.String())

	config := pubsub.SubscriptionConfig{
		Topic:            s.topic,
		Filter:           fmt.Sprintf("attributes.%s = \"%s\"", tripIDAttribute, tripId.String()),
		ExpirationPolicy: 24 * time.Hour, // Set a reasonable expiration policy
		AckDeadline:      10 * time.Second,
	}

	gcpSub, err := s.client.CreateSubscription(s.ctx, gcpSubName, config)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("failed to create GCP subscription %s for %s: %w", gcpSubName, typeName, err)
	}

	msgChan := make(chan M, 5)
	// Create a cancellable context for the receiver goroutine.
	receiveCtx, cancel := context.WithCancel(s.ctx)

	s.subscriptionsMutex.Lock()
	s.activeSubscriptions[subscriptionID] = &subscriptionInfo{
		gcpSubscription: gcpSub,
		cancel:          cancel,
	}
	s.subscriptionsMutex.Unlock()

	go func() {
		// Automatically clean up when the goroutine exits.
		defer func() {
			s.subscriptionsMutex.Lock()
			delete(s.activeSubscriptions, subscriptionID)
			s.subscriptionsMutex.Unlock()

			// Delete the subscription from GCP to prevent resource leaks.
			if deleteErr := gcpSub.Delete(context.Background()); deleteErr != nil {
				log.Printf("Error deleting GCP subscription %s: %v", gcpSub.ID(), deleteErr)
			} else {
				// log.Printf("Deleted GCP subscription: %s", gcpSub.ID())
			}
			close(msgChan)
			// log.Printf("Goroutine for %s subscription %s shut down.", typeName, subscriptionID)
		}()

		// Receive blocks until the context is cancelled.
		err := gcpSub.Receive(receiveCtx, func(ctx context.Context, pubsubMsg *pubsub.Message) {
			pubsubMsg.Ack()

			var msg M
			if err := json.Unmarshal(pubsubMsg.Data, &msg); err != nil {
				log.Printf("Error unmarshaling %s for %s: %v. Body: %s", typeName, subscriptionID, err, string(pubsubMsg.Data))
				return
			}

			select {
			case msgChan <- msg:
			case <-time.After(2 * time.Second):
				log.Printf("Timeout sending %s to msgChan for %s.", typeName, subscriptionID)
			case <-receiveCtx.Done(): // Check if we were cancelled while trying to send.
				return
			}
		})

		if err != nil && err != context.Canceled {
			log.Printf("Error in Receive loop for %s subscription %s: %v", typeName, subscriptionID, err)
		}
	}()

	// log.Printf("Subscribed to %s for tripId %s. Subscription ID: %s", typeName, tripId, subscriptionID)
	return subscriptionID, msgChan, nil
}

// DeSubscribe stops the message receiver and deletes the subscription from GCP.
func (s *GenericPubSubService[M]) DeSubscribe(id uuid.UUID) error {
	s.subscriptionsMutex.Lock()
	info, ok := s.activeSubscriptions[id]
	if ok {
		// It's removed from the map inside the goroutine's defer block.
		// Here we just trigger the cancellation.
		info.cancel()
	}
	s.subscriptionsMutex.Unlock()

	if !ok {
		return fmt.Errorf("subscription ID %s not found for %s service", id, reflect.TypeOf(*new(M)).Name())
	}

	// log.Printf("DeSubscribing %s subscription ID %s", reflect.TypeOf(*new(M)).Name(), id)
	return nil
}

// Close gracefully shuts down all active subscriptions for this service.
func (s *GenericPubSubService[M]) Close() {
	s.subscriptionsMutex.Lock()
	defer s.subscriptionsMutex.Unlock()

	for _, info := range s.activeSubscriptions {
		// log.Printf("Closing consumer for subscription ID %s", id)
		info.cancel()
	}
}

// --- tripRecordMQ implementation ---
type tripRecordMQ struct {
	genericService *GenericPubSubService[mq.TripRecordMessage]
	action         mq.Action
}

func NewTripRecordMessageQueue(ctx context.Context, client *pubsub.Client, action mq.Action) (*tripRecordMQ, error) {
	topicID := fmt.Sprintf("trip-record-%s", action.String())
	gs, err := NewGenericPubSubService[mq.TripRecordMessage](ctx, client, topicID)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic service for TripRecord: %w", err)
	}
	return &tripRecordMQ{genericService: gs, action: action}, nil
}
func (q *tripRecordMQ) GetAction() mq.Action                   { return q.action }
func (q *tripRecordMQ) Publish(msg mq.TripRecordMessage) error { return q.genericService.Publish(msg) }
func (q *tripRecordMQ) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	return q.genericService.Subscribe(tripId)
}
func (q *tripRecordMQ) DeSubscribe(id uuid.UUID) error { return q.genericService.DeSubscribe(id) }

// --- tripAddressMQ implementation ---
type tripAddressMQ struct {
	genericService *GenericPubSubService[mq.TripAddressMessage]
	action         mq.Action
}

func NewTripAddressMessageQueue(ctx context.Context, client *pubsub.Client, action mq.Action) (*tripAddressMQ, error) {
	topicID := fmt.Sprintf("trip-address-%s", action.String())
	gs, err := NewGenericPubSubService[mq.TripAddressMessage](ctx, client, topicID)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic service for TripAddress: %w", err)
	}
	return &tripAddressMQ{genericService: gs, action: action}, nil
}
func (q *tripAddressMQ) GetAction() mq.Action { return q.action }
func (q *tripAddressMQ) Publish(msg mq.TripAddressMessage) error {
	return q.genericService.Publish(msg)
}
func (q *tripAddressMQ) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	return q.genericService.Subscribe(tripId)
}
func (q *tripAddressMQ) DeSubscribe(id uuid.UUID) error { return q.genericService.DeSubscribe(id) }

// --------- trip message queue wrapper implementation ---------

type GCPTripMessageQueueWrapper struct {
	RecordMQArray  [mq.ActionCnt]*tripRecordMQ
	AddressMQArray [mq.ActionCnt]*tripAddressMQ
}

func (wrapper *GCPTripMessageQueueWrapper) GetTripRecordMessageQueue(action mq.Action) mq.TripRecordMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil
	}
	return wrapper.RecordMQArray[action]
}

func (wrapper *GCPTripMessageQueueWrapper) GetTripAddressMessageQueue(action mq.Action) mq.TripAddressMessageQueue {
	if action < 0 || action >= mq.ActionCnt || wrapper.AddressMQArray[action] == nil {
		return nil
	}
	return wrapper.AddressMQArray[action]
}

// NewGCPTripMessageQueueWrapper creates a new MQ wrapper instance using GCP Pub/Sub.
func NewGCPTripMessageQueueWrapper(ctx context.Context, projectID string) (mq.TripMessageQueueWrapper, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP Pub/Sub client for project %s: %w", projectID, err)
	}

	wrapper := &GCPTripMessageQueueWrapper{}

	// Address: Create, Delete
	wrapper.AddressMQArray[mq.ActionCreate], err = NewTripAddressMessageQueue(ctx, client, mq.ActionCreate)
	if err != nil {
		return nil, err
	}
	wrapper.AddressMQArray[mq.ActionUpdate] = nil // Not implemented for Address
	wrapper.AddressMQArray[mq.ActionDelete], err = NewTripAddressMessageQueue(ctx, client, mq.ActionDelete)
	if err != nil {
		return nil, err
	}

	// Record: Create, Update, Delete
	wrapper.RecordMQArray[mq.ActionCreate], err = NewTripRecordMessageQueue(ctx, client, mq.ActionCreate)
	if err != nil {
		return nil, err
	}
	wrapper.RecordMQArray[mq.ActionUpdate], err = NewTripRecordMessageQueue(ctx, client, mq.ActionUpdate)
	if err != nil {
		return nil, err
	}
	wrapper.RecordMQArray[mq.ActionDelete], err = NewTripRecordMessageQueue(ctx, client, mq.ActionDelete)
	if err != nil {
		return nil, err
	}

	return wrapper, nil
}
