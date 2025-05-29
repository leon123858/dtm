package goch

import (
	"dtm/db/db" // Assuming dtm/db/db contains your Address struct
	"dtm/mq/mq" // Assuming your mq package is here

	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestGoChanTripMessageQueueWrapper tests the wrapper's ability to create queues.
func TestGoChanTripMessageQueueWrapper(t *testing.T) {
	wrapper := NewGoChanTripMessageQueueWrapper()
	if wrapper == nil {
		t.Fatal("NewGoChanTripMessageQueueWrapper returned nil")
	}

	// Assert it's the correct underlying type and action
	if _, ok := wrapper.(*GoChanTripMessageQueueWrapper); !ok {
		t.Errorf("NewGoChanTripMessageQueueWrapper did not return *GoChanTripMessageQueueWrapper")
	}
	// Test GetTripRecordMessageQueue for create Action
	recordQueue := wrapper.GetTripRecordMessageQueue(mq.ActionCreate)
	if recordQueue == nil {
		t.Error("GetTripRecordMessageQueue returned nil for ActionCreate")
	}
	// Assert it's the correct underlying type and action
	if chRecordQ, ok := recordQueue.(*ChannelTripRecordMessageQueue); !ok {
		t.Errorf("GetTripRecordMessageQueue did not return *ChannelTripRecordMessageQueue")
	} else {
		if chRecordQ.GetAction() != mq.ActionCreate {
			t.Errorf("Expected record queue action %v, got %v", mq.ActionCreate, chRecordQ.GetAction())
		}
		if cap(chRecordQ.channel) != 0 {
			t.Errorf("Expected record queue capacity 0, got %d", cap(chRecordQ.channel))
		}
	}
}

// TestNewChannelTripRecordMessageQueue tests the constructor for record queue.
func TestNewChannelTripRecordMessageQueue(t *testing.T) {
	// Test unbuffered channel
	qUnbuffered := NewChannelTripRecordMessageQueue(mq.ActionCreate, 0)
	if qUnbuffered == nil {
		t.Fatal("NewChannelTripRecordMessageQueue returned nil for unbuffered")
	}
	if qUnbuffered.GetAction() != mq.ActionCreate {
		t.Errorf("Expected action %v, got %v", mq.ActionCreate, qUnbuffered.GetAction())
	}
	if cap(qUnbuffered.channel) != 0 {
		t.Errorf("Expected unbuffered channel capacity 0, got %d", cap(qUnbuffered.channel))
	}

	// Test buffered channel
	qBuffered := NewChannelTripRecordMessageQueue(mq.ActionUpdate, 5)
	if qBuffered == nil {
		t.Fatal("NewChannelTripRecordMessageQueue returned nil for buffered")
	}
	if qBuffered.GetAction() != mq.ActionUpdate {
		t.Errorf("Expected action %v, got %v", mq.ActionUpdate, qBuffered.GetAction())
	}
	if cap(qBuffered.channel) != 5 {
		t.Errorf("Expected buffered channel capacity 5, got %d", cap(qBuffered.channel))
	}
}

// TestChannelTripRecordMessageQueue_PublishAndSubscribe tests basic publish/subscribe for record queue.
func TestChannelTripRecordMessageQueue_PublishAndSubscribe(t *testing.T) {
	q := NewChannelTripRecordMessageQueue(mq.ActionCreate, 1) // Buffered channel for predictable test

	// Subscribe to the channel
	subCh, err := q.Subscribe()
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	testMsg := mq.TripRecordMessage{
		ID:            uuid.New(),
		Name:          "Test Trip",
		Amount:        123.45,
		PrePayAddress: db.Address("testAddress"),
	}

	// Publish a message
	err = q.Publish(testMsg)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Read from the subscribed channel
	select {
	case receivedMsg := <-subCh:
		if receivedMsg.ID != testMsg.ID {
			t.Errorf("Expected ID %v, got %v", testMsg.ID, receivedMsg.ID)
		}
		if receivedMsg.Name != testMsg.Name {
			t.Errorf("Expected Name %s, got %s", testMsg.Name, receivedMsg.Name)
		}
		if receivedMsg.Amount != testMsg.Amount {
			t.Errorf("Expected Amount %f, got %f", testMsg.Amount, receivedMsg.Amount)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for message")
	}
}

// TestChannelTripRecordMessageQueue_PublishFullQueue tests publishing to a full queue.
func TestChannelTripRecordMessageQueue_PublishFullQueue(t *testing.T) {
	q := NewChannelTripRecordMessageQueue(mq.ActionCreate, 1) // Buffer size 1

	testMsg1 := mq.TripRecordMessage{ID: uuid.New()}
	testMsg2 := mq.TripRecordMessage{ID: uuid.New()}

	// Publish first message (fills the buffer)
	err := q.Publish(testMsg1)
	if err != nil {
		t.Fatalf("Failed to publish first message: %v", err)
	}

	// Publish second message (should fail as buffer is full)
	err = q.Publish(testMsg2)
	if err == nil {
		t.Fatal("Expected ErrQueueFull, but no error was returned")
	}
	if err != ErrQueueFull {
		t.Errorf("Expected error %v, got %v", ErrQueueFull, err)
	}

	// Consume the first message to clear the queue
	select {
	case <-q.channel:
		// Message consumed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting to consume message from full queue")
	}

	// Publish again, should succeed now
	err = q.Publish(testMsg2)
	if err != nil {
		t.Fatalf("Failed to publish after consuming: %v", err)
	}
}

// TestChannelTripRecordMessageQueue_ConcurrentPublishAndSubscribe tests concurrent operations.
func TestChannelTripRecordMessageQueue_ConcurrentPublishAndSubscribe(t *testing.T) {
	const (
		numMessages = 100
		bufferSize  = 10
	)
	q := NewChannelTripRecordMessageQueue(mq.ActionCreate, bufferSize)
	subCh, err := q.Subscribe()
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	var sentMsgs sync.Map // To track sent messages
	var wg sync.WaitGroup

	// Publisher goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			// generate fix uuid by i
			uid := uuid.NewSHA1(uuid.NameSpaceDNS, []byte("test-trip-record-"+string(rune(i))))
			msg := mq.TripRecordMessage{
				ID:   uid,
				Name: "Concurrent Test",
			}
			sentMsgs.Store(msg.ID, true) // Store ID to verify later
			err := q.Publish(msg)
			if err != nil {
				// We expect ErrQueueFull sometimes if consumer is slow,
				// but this test aims to check overall functionality.
				// For a more robust test, you might retry or ensure buffer never fills.
				if err == ErrQueueFull {
					t.Logf("Publisher: Queue full, retrying or skipping message %d", i)
					time.Sleep(1 * time.Millisecond) // Give consumer a chance
					i--                              // Retry this message
					continue
				}
				t.Errorf("Publisher: Unexpected error publishing message %d: %v", i, err)
			}
		}
	}()

	// Consumer goroutine
	receivedCount := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		for receivedCount < numMessages {
			select {
			case msg := <-subCh:
				if _, loaded := sentMsgs.LoadAndDelete(msg.ID); !loaded {
					t.Errorf("Consumer: Received unexpected or duplicate message ID: %v", msg.ID)
				}
				receivedCount++
			case <-time.After(500 * time.Millisecond):
				// If no messages for a while, it might mean publisher is done
				// or there's a deadlock. Break after a timeout.
				break
			}
		}
	}()

	wg.Wait() // Wait for publisher and consumer to complete

	// Check if all messages were received
	if receivedCount != numMessages {
		t.Errorf("Expected %d messages, received %d", numMessages, receivedCount)
	}
	if count := countMap(&sentMsgs); count > 0 {
		t.Errorf("There are %d messages that were sent but not received.", count)
	}
}

// Helper to count items in a sync.Map
func countMap(m *sync.Map) int {
	count := 0
	m.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// TestNewChannelTripAddressMessageQueue tests the constructor for address queue.
func TestNewChannelTripAddressMessageQueue(t *testing.T) {
	// Test unbuffered channel
	qUnbuffered := NewChannelTripAddressMessageQueue(mq.ActionDelete, 0)
	if qUnbuffered == nil {
		t.Fatal("NewChannelTripAddressMessageQueue returned nil for unbuffered")
	}
	if qUnbuffered.GetAction() != mq.ActionDelete {
		t.Errorf("Expected action %v, got %v", mq.ActionDelete, qUnbuffered.GetAction())
	}
	if cap(qUnbuffered.channel) != 0 {
		t.Errorf("Expected unbuffered channel capacity 0, got %d", cap(qUnbuffered.channel))
	}

	// Test buffered channel
	qBuffered := NewChannelTripAddressMessageQueue(mq.ActionCreate, 3)
	if qBuffered == nil {
		t.Fatal("NewChannelTripAddressMessageQueue returned nil for buffered")
	}
	if qBuffered.GetAction() != mq.ActionCreate {
		t.Errorf("Expected action %v, got %v", mq.ActionCreate, qBuffered.GetAction())
	}
	if cap(qBuffered.channel) != 3 {
		t.Errorf("Expected buffered channel capacity 3, got %d", cap(qBuffered.channel))
	}
}

// TestChannelTripAddressMessageQueue_PublishAndSubscribe tests basic publish/subscribe for address queue.
func TestChannelTripAddressMessageQueue_PublishAndSubscribe(t *testing.T) {
	q := NewChannelTripAddressMessageQueue(mq.ActionUpdate, 1)

	subCh, err := q.Subscribe()
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	testMsg := mq.TripAddressMessage{
		Address: db.Address("testAddress"),
	}

	err = q.Publish(testMsg)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	select {
	case receivedMsg := <-subCh:
		if receivedMsg.Address != db.Address("testAddress") {
			t.Errorf("Expected Street %s, got %s", testMsg.Address, receivedMsg.Address)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for message")
	}
}

// TestChannelTripAddressMessageQueue_PublishFullQueue tests publishing to a full address queue.
func TestChannelTripAddressMessageQueue_PublishFullQueue(t *testing.T) {
	q := NewChannelTripAddressMessageQueue(mq.ActionDelete, 1)

	testMsg1 := mq.TripAddressMessage{Address: db.Address("testAddress")}
	testMsg2 := mq.TripAddressMessage{Address: db.Address("testAddress2")}

	err := q.Publish(testMsg1)
	if err != nil {
		t.Fatalf("Failed to publish first message: %v", err)
	}

	err = q.Publish(testMsg2)
	if err == nil {
		t.Fatal("Expected ErrQueueFull, but no error was returned")
	}
	if err != ErrQueueFull {
		t.Errorf("Expected error %v, got %v", ErrQueueFull, err)
	}

	// Consume the first message to clear the queue
	select {
	case <-q.channel:
		// Message consumed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting to consume message from full queue")
	}

	// Publish again, should succeed now
	err = q.Publish(testMsg2)
	if err != nil {
		t.Fatalf("Failed to publish after consuming: %v", err)
	}
}
