package goch

import (
	"dtm/mq/mq" // Assuming this path is correct for your mq interfaces and types
	"fmt"
	"sync"
	"time" // For potential timeouts in fan-out

	"github.com/google/uuid"
)

// --- Generic Fan-Out Queue Core ---

// TopicProvider 定義了一個可以提供 Topic ID 的介面
type TopicProvider interface {
	GetTopic() uuid.UUID
}

type Subscriber[T any] struct {
	TripID  uuid.UUID
	Channel chan T
}

// fanOutQueueCore provides the generic fan-out logic for any message type.
type fanOutQueueCore[T TopicProvider] struct {
	publishChan chan T                      // Main channel for incoming messages
	subscribers map[uuid.UUID]Subscriber[T] // Map of subscriberID to subscriber channel
	mu          sync.RWMutex                // Protects the subscribers map
	quit        chan struct{}               // Signal to stop the fan-out goroutine
	wg          sync.WaitGroup              // WaitGroup for the fan-out goroutine
	bufferSize  int                         // Buffer size for the main publish channel
}

// newFanOutQueueCore creates a new instance of fanOutQueueCore.
func newFanOutQueueCore[T TopicProvider](bufferSize int) *fanOutQueueCore[T] {
	var pubChan chan T
	if bufferSize > 0 {
		pubChan = make(chan T, bufferSize)
	} else {
		pubChan = make(chan T) // Unbuffered
	}

	core := &fanOutQueueCore[T]{
		publishChan: pubChan,
		subscribers: make(map[uuid.UUID]Subscriber[T]),
		quit:        make(chan struct{}),
		bufferSize:  bufferSize,
		mu:          sync.RWMutex{},
		wg:          sync.WaitGroup{},
	}

	core.wg.Add(1)
	go core.startFanOutRoutine() // Start the fan-out goroutine
	return core
}

// Publish sends a message to the main channel.
// This is the input point for messages to be fanned out.
func (f *fanOutQueueCore[T]) Publish(msg T) error {
	select {
	case f.publishChan <- msg:
		// 如果 channel 有緩衝空間，或者有無緩衝 channel 的接收者立即準備好，則發送成功
		return nil
	case <-time.After(200 * time.Millisecond):
		return FullQueueError
	}
}

// Subscribe adds a new subscriber and returns its channel and ID.
func (f *fanOutQueueCore[T]) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan T, error) {
	var subChan chan T
	if f.bufferSize > 0 {
		subChan = make(chan T, f.bufferSize)
	} else {
		subChan = make(chan T)
	}
	subscriberID := uuid.New()

	f.mu.Lock()
	defer f.mu.Unlock()

	f.subscribers[subscriberID] = Subscriber[T]{
		TripID:  tripId,
		Channel: subChan,
	}
	// fmt.Printf("goch: New subscriber with ID '%s' added.\n", subscriberID)
	return subscriberID, subChan, nil
}

// DeSubscribe removes a subscriber by its ID and closes its channel.
func (f *fanOutQueueCore[T]) DeSubscribe(subscriberID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if ch, ok := f.subscribers[subscriberID]; ok {
		delete(f.subscribers, subscriberID)
		close(ch.Channel) // Important: Close the subscriber's channel
		// fmt.Printf("goch: Subscriber with ID '%s' removed and its channel closed.\n", subscriberID)
		return nil
	}
	return fmt.Errorf("goch: subscriber with ID '%s' not found", subscriberID)
}

// Stop signals the fan-out goroutine to shut down and waits for it.
func (f *fanOutQueueCore[T]) Stop() {
	close(f.publishChan) // Closing the publish channel will end the fan-out routine's loop
	f.wg.Wait()          // Wait for the fan-out routine to finish
	// fmt.Println("goch: Fan-out queue stopped.")
}

// startFanOutRoutine handles fanning out messages from the publishChan to subscribers.
func (f *fanOutQueueCore[T]) startFanOutRoutine() {
	defer f.wg.Done()

	for msg := range f.publishChan { // Loop exits when publishChan is closed
		f.mu.RLock() // Acquire read lock to safely read the subscribers map

		subscribersSnapshot := make(map[uuid.UUID]chan T)
		for id, ch := range f.subscribers {
			if ch.TripID == msg.GetTopic() { // Only include subscribers for the specific trip ID
				subscribersSnapshot[id] = ch.Channel // Copy the channel to avoid holding the lock while sending
			}
		}
		f.mu.RUnlock() // Release read lock

		var failedSubscribers []uuid.UUID // Collect IDs of subscribers that failed to receive

		for id, subChan := range subscribersSnapshot {
			select {
			case subChan <- msg:
				// Message sent successfully
			case <-time.After(50 * time.Millisecond): // Optional: Add a timeout for slow consumers
				// fmt.Printf("goch: Warning: Timed out sending message to subscriber ID '%s'. Channel might be blocked.\n", id)
				failedSubscribers = append(failedSubscribers, id)
			default:
				// Channel is blocked or closed (sending to a closed channel with select default won't panic, it just goes to default)
				// fmt.Printf("goch: Warning: Failed to send message to subscriber ID '%s'. Channel is full or closed.\n", id)
				failedSubscribers = append(failedSubscribers, id)
			}
		}

		// After iterating, remove failed subscribers (if any).
		if len(failedSubscribers) > 0 {
			for _, id := range failedSubscribers {
				_ = f.DeSubscribe(id)
			}
		}
	}
	// fmt.Println("goch: Fan-out routine exiting.")
}

// --- Specific Message Queue Implementations ---

// ChannelTripRecordMessageQueue implements TripRecordMessageQueue using a Go channel.
type ChannelTripRecordMessageQueue struct {
	action mq.Action
	core   *fanOutQueueCore[mq.TripRecordMessage] // Embed the generic core
}

// NewChannelTripRecordMessageQueue creates a new instance of ChannelTripRecordMessageQueue.
func NewChannelTripRecordMessageQueue(action mq.Action, bufferSize int) *ChannelTripRecordMessageQueue {
	return &ChannelTripRecordMessageQueue{
		action: action,
		core:   newFanOutQueueCore[mq.TripRecordMessage](bufferSize),
	}
}

// GetAction returns the action associated with this queue.
func (q *ChannelTripRecordMessageQueue) GetAction() mq.Action {
	return q.action
}

// Publish sends a TripRecordMessage to the queue.
func (q *ChannelTripRecordMessageQueue) Publish(msg mq.TripRecordMessage) error {
	return q.core.Publish(msg)
}

// Subscribe returns a read-only channel for TripRecordMessages.
func (q *ChannelTripRecordMessageQueue) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	uid, subChan, err := q.core.Subscribe(tripId) // Delegate to the core's Subscribe
	return uid, subChan, err
}

func (q *ChannelTripRecordMessageQueue) DeSubscribe(subscriberID uuid.UUID) error {
	return q.core.DeSubscribe(subscriberID)
}

// Stop stops the underlying core fan-out routine.
func (q *ChannelTripRecordMessageQueue) Stop() {
	q.core.Stop()
}

// ChannelTripAddressMessageQueue implements TripAddressMessageQueue using a Go channel.
type ChannelTripAddressMessageQueue struct {
	action mq.Action
	core   *fanOutQueueCore[mq.TripAddressMessage] // Embed the generic core
}

// NewChannelTripAddressMessageQueue creates a new instance of ChannelTripAddressMessageQueue.
func NewChannelTripAddressMessageQueue(action mq.Action, bufferSize int) *ChannelTripAddressMessageQueue {
	return &ChannelTripAddressMessageQueue{
		action: action,
		core:   newFanOutQueueCore[mq.TripAddressMessage](bufferSize),
	}
}

// GetAction returns the action associated with this queue.
func (q *ChannelTripAddressMessageQueue) GetAction() mq.Action {
	return q.action
}

// Publish sends a TripAddressMessage to the queue.
func (q *ChannelTripAddressMessageQueue) Publish(msg mq.TripAddressMessage) error {
	q.core.Publish(msg) // Delegate to the core's Publish
	return nil
}

// Subscribe returns a read-only channel for TripAddressMessages.
func (q *ChannelTripAddressMessageQueue) Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	uid, subChan, err := q.core.Subscribe(tripId) // Delegate to the core's Subscribe
	return uid, subChan, err
}

// DeSubscribe removes a subscriber channel.
func (q *ChannelTripAddressMessageQueue) DeSubscribe(subscriberID uuid.UUID) error {
	return q.core.DeSubscribe(subscriberID)
}

// Stop stops the underlying core fan-out routine.
func (q *ChannelTripAddressMessageQueue) Stop() {
	q.core.Stop()
}

// --- Wrapper for Message Queues ---

// This struct can be used to implement the TripMessageQueueWrapper interface
type GoChanTripMessageQueueWrapper struct {
	RecordMQArray  [mq.ActionCnt]*ChannelTripRecordMessageQueue  // Use pointers to the new struct
	AddressMQArray [mq.ActionCnt]*ChannelTripAddressMessageQueue // Use pointers to the new struct
}

func (wrapper *GoChanTripMessageQueueWrapper) GetTripRecordMessageQueue(action mq.Action) mq.TripRecordMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle the error as needed
	}
	return wrapper.RecordMQArray[action]
}

func (wrapper *GoChanTripMessageQueueWrapper) GetTripAddressMessageQueue(action mq.Action) mq.TripAddressMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle the error as needed
	}
	if wrapper.AddressMQArray[action] == nil {
		return nil
	}
	return wrapper.AddressMQArray[action]
}

// NewGoChanTripMessageQueueWrapper creates a new instance of GoChanTripMessageQueueWrapper.
func NewGoChanTripMessageQueueWrapper() mq.TripMessageQueueWrapper {
	wrapper := GoChanTripMessageQueueWrapper{}
	// address need add and remove
	wrapper.AddressMQArray[mq.ActionCreate] = NewChannelTripAddressMessageQueue(mq.ActionCreate, 0)
	wrapper.AddressMQArray[mq.ActionUpdate] = nil
	wrapper.AddressMQArray[mq.ActionDelete] = NewChannelTripAddressMessageQueue(mq.ActionDelete, 0)
	// record need add, update and delete
	wrapper.RecordMQArray[mq.ActionCreate] = NewChannelTripRecordMessageQueue(mq.ActionCreate, 0)
	wrapper.RecordMQArray[mq.ActionUpdate] = NewChannelTripRecordMessageQueue(mq.ActionUpdate, 0)
	wrapper.RecordMQArray[mq.ActionDelete] = NewChannelTripRecordMessageQueue(mq.ActionDelete, 0)

	return &wrapper
}

// --- Error Definitions ---
// Note: These errors are less relevant now that `Publish` only indicates acceptance into the queue.
// If you need fan-out specific errors, consider a more complex return from `Publish` or a separate error channel.
type QueueError string

func (e QueueError) Error() string {
	return string(e)
}

const (
	FullQueueError QueueError = "main queue is full"
)
