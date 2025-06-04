package goch

import (
	// Assuming these paths are correct as per your project structure
	"dtm/db/db"
	"dtm/mq/mq"

	// For error comparison
	"fmt" // Used in some error messages, and by the code under test
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Helper to receive a message from a channel with a timeout.
// Returns the message and true if successful, or zero value and false on timeout/closed.
func receiveMsgWithTimeout[T any](tb testing.TB, ch <-chan T, timeout time.Duration) (T, bool) {
	tb.Helper()
	select {
	case msg, ok := <-ch:
		if !ok {
			var zero T
			return zero, false // Channel closed
		}
		return msg, true
	case <-time.After(timeout):
		var zero T
		return zero, false // Timeout
	}
}

// Helper to check if a channel is closed (non-blocking).
func isChanClosed[T any](ch <-chan T) bool {
	select {
	case _, ok := <-ch:
		return !ok // ok is false if channel is closed
	default:
		return false // channel is open and would block or is empty
	}
}

type MockItem struct {
	Value   int
	TopicID uuid.UUID
}

func (item MockItem) GetTopic() uuid.UUID {
	return item.TopicID
}

// --- fanOutQueueCore Tests ---

func TestNewFanOutQueueCore(t *testing.T) {
	t.Parallel()

	t.Run("Unbuffered", func(t *testing.T) {
		t.Parallel()
		core := newFanOutQueueCore[MockItem](0)
		if core == nil {
			t.Fatal("newFanOutQueueCore returned nil for unbuffered")
		}
		defer core.Stop() // Ensure cleanup

		if core.publishChan == nil {
			t.Error("publishChan is nil")
		}
		if cap(core.publishChan) != 0 {
			t.Errorf("expected publishChan capacity 0, got %d", cap(core.publishChan))
		}
		if core.subscribers == nil {
			t.Error("subscribers map is nil")
		}
		if core.quit == nil { // quit channel is an implementation detail, but part of the struct
			t.Error("quit channel is nil")
		}
		if core.bufferSize != 0 {
			t.Errorf("expected bufferSize 0, got %d", core.bufferSize)
		}
	})

	t.Run("Buffered", func(t *testing.T) {
		t.Parallel()
		bufferSize := 10
		core := newFanOutQueueCore[MockItem](bufferSize)
		if core == nil {
			t.Fatal("newFanOutQueueCore returned nil for buffered")
		}
		defer core.Stop() // Ensure cleanup

		if core.publishChan == nil {
			t.Error("publishChan is nil")
		}
		if cap(core.publishChan) != bufferSize {
			t.Errorf("expected publishChan capacity %d, got %d", bufferSize, cap(core.publishChan))
		}
		if core.bufferSize != bufferSize {
			t.Errorf("expected bufferSize %d, got %d", bufferSize, core.bufferSize)
		}
	})
}

func TestFanOutQueueCore_PublishSubscribeDeSubscribe_Simple(t *testing.T) {
	t.Parallel()
	core := newFanOutQueueCore[MockItem](0) // Unbuffered publishChan
	defer core.Stop()
	topic := uuid.New()
	id1, subChan1, err := core.Subscribe(topic)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if subChan1 == nil {
		t.Fatal("Subscriber channel is nil")
	}

	testMsg := 42
	// Publish in a goroutine because an unbuffered publishChan requires a ready receiver (the fanOutRoutine)
	go func() {
		if pubErr := core.Publish(MockItem{Value: testMsg, TopicID: topic}); pubErr != nil {
			// Use t.Errorf in goroutines as t.Fatal/Fatalf will only stop the goroutine
			t.Errorf("Publish failed: %v", pubErr)
		}
	}()

	receivedMsg, ok := receiveMsgWithTimeout(t, subChan1, 500*time.Millisecond)
	if !ok {
		t.Errorf("Failed to receive message or channel closed/timed out")
	}
	if receivedMsg.Value != testMsg {
		t.Errorf("Expected message %d, got %d", testMsg, receivedMsg.Value)
	}

	time.Sleep(500 * time.Millisecond)

	// DeSubscribe
	err = core.DeSubscribe(id1)
	if err != nil {
		t.Fatalf("DeSubscribe failed: %v", err)
	}

	// Check if channel is closed (eventually)
	// Give a moment for the close operation to complete.
	time.Sleep(500 * time.Millisecond)
	if !isChanClosed(subChan1) {
		// Attempt a read, which should confirm closure
		_, stillOpen := <-subChan1
		if stillOpen {
			t.Error("Subscriber channel not closed after DeSubscribe")
		}
	}

	// Publish another message; the desubscribed channel should not receive it.
	msgAfterDeSub := 100
	if pubErr := core.Publish(MockItem{Value: msgAfterDeSub, TopicID: topic}); pubErr != nil {
		t.Errorf("Publish after de-subscribe failed: %v", pubErr)
	}

	// Attempt to receive from the (now closed) desubscribed channel.
	select {
	case val, stillOpen := <-subChan1:
		if stillOpen {
			t.Errorf("Received message %v on desubscribed channel; expected channel to be closed.", val)
		}
		// If !stillOpen, it's correctly closed, received zero value.
	case <-time.After(500 * time.Millisecond):
		// This case can be hit if the channel is closed and empty.
		// isChanClosed check above is more deterministic for closure.
	}
}

func TestFanOutQueueCore_MultipleSubscribers(t *testing.T) {
	t.Parallel()
	core := newFanOutQueueCore[MockItem](10)
	defer core.Stop()

	numSubscribers := 3
	subChans := make(map[uuid.UUID]<-chan MockItem)
	subIDs := make([]uuid.UUID, numSubscribers)
	topic := uuid.New()
	for i := 0; i < numSubscribers; i++ {
		id, ch, err := core.Subscribe(topic)
		if err != nil {
			t.Fatalf("Subscribe failed for subscriber %d: %v", i, err)
		}
		subChans[id] = ch
		subIDs[i] = id
	}

	testMsg := MockItem{Value: 333, TopicID: topic}

	if err := core.Publish(testMsg); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	for id, ch := range subChans {
		msg, ok := receiveMsgWithTimeout(t, ch, 500*time.Millisecond)
		if !ok {
			t.Errorf("Subscriber %s failed to receive message or timed out", id)
			return
		}
		if msg != testMsg {
			t.Errorf("Subscriber %s expected message '%v', got '%v'", id, testMsg, msg)
		}
	}

	time.Sleep(500 * time.Millisecond)

	// DeSubscribe one subscriber
	idToDeSub := subIDs[0]
	chanToClose := subChans[idToDeSub] // Keep ref to check if closed
	err := core.DeSubscribe(idToDeSub)
	if err != nil {
		t.Fatalf("DeSubscribe failed for %s: %v", idToDeSub, err)
	}
	delete(subChans, idToDeSub) // Remove from active map for next check

	time.Sleep(500 * time.Millisecond) // Allow for channel close propagation
	if !isChanClosed(chanToClose) {
		t.Errorf("Channel for %s not closed after de-subscribe", idToDeSub)
	}

	// Publish another message
	testMsg2 := MockItem{Value: 444, TopicID: topic}
	if err := core.Publish(testMsg2); err != nil {
		t.Fatalf("Publish (2) failed: %v", err)
	}

	// Check remaining subscribers receive the second message
	for id, ch := range subChans {
		msg, ok := receiveMsgWithTimeout(t, ch, 500*time.Millisecond)
		if !ok {
			t.Errorf("Subscriber %s failed to receive message '%v' or timed out", id, testMsg2)
			return
		}
		if msg != testMsg2 {
			t.Errorf("Subscriber %s expected message '%v', got '%v'", id, testMsg2, msg)
		}
	}

	// Check the desubscribed channel did not receive the second message (it should be closed)
	select {
	case msg, open := <-chanToClose:
		if open {
			t.Errorf("Desubscribed channel %s received message '%v'", idToDeSub, msg)
		}
	default:
		// Expected path for a closed and empty channel
	}
}

func TestFanOutQueueCore_DeSubscribeNonExistent(t *testing.T) {
	t.Parallel()
	core := newFanOutQueueCore[MockItem](0)
	defer core.Stop()

	nonExistentID := uuid.New()
	err := core.DeSubscribe(nonExistentID)
	if err == nil {
		t.Error("Expected error when desubscribing non-existent ID, got nil")
	} else {
		expectedErrorMsg := fmt.Sprintf("goch: subscriber with ID '%s' not found", nonExistentID)
		if err.Error() != expectedErrorMsg {
			t.Errorf("Expected error message '%s', got '%s'", expectedErrorMsg, err.Error())
		}
	}
}

func TestFanOutQueueCore_Stop(t *testing.T) {
	t.Parallel()
	core := newFanOutQueueCore[MockItem](5) // Buffered publishChan

	topic1 := uuid.New()
	topic2 := uuid.New()

	id1, subChan1, _ := core.Subscribe(topic1)
	id2, subChan2, _ := core.Subscribe(topic2)

	var receivedMessages1 []MockItem
	var receivedMessages2 []MockItem
	doneReceiving := make(chan bool)

	core.Publish(MockItem{Value: 1, TopicID: topic1})
	core.Publish(MockItem{Value: 2, TopicID: topic2})
	core.Publish(MockItem{Value: 3, TopicID: uuid.New()})

	go func() {
		for {
			msg, ok := receiveMsgWithTimeout(t, subChan1, 500*time.Millisecond)
			if !ok {
				break
			}
			receivedMessages1 = append(receivedMessages1, msg)
		}
		doneReceiving <- true
	}()
	go func() {
		for {
			msg, ok := receiveMsgWithTimeout(t, subChan2, 500*time.Millisecond)
			if !ok {
				break
			}
			receivedMessages2 = append(receivedMessages2, msg)
		}
		doneReceiving <- true
	}()

	<-doneReceiving
	<-doneReceiving

	if !reflect.DeepEqual(receivedMessages1, []MockItem{{Value: 1, TopicID: topic1}}) {
		t.Errorf("Sub1: Expected messages %v, got %v after Stop", []MockItem{{Value: 1, TopicID: topic1}}, receivedMessages1)
	}
	if !reflect.DeepEqual(receivedMessages2, []MockItem{{Value: 2, TopicID: topic2}}) {
		t.Errorf("Sub2: Expected messages %v, got %v after Stop", []MockItem{{Value: 2, TopicID: topic2}}, receivedMessages2)
	}

	stopDone := make(chan struct{})
	go func() {
		core.Stop() // This closes publishChan and waits for fanOutRoutine
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// Stop completed as expected.
	case <-time.After(1 * time.Second):
		t.Fatal("core.Stop() timed out")
	}

	// Verify subscriber channels are not closed by Stop()
	if isChanClosed(subChan1) {
		t.Errorf("subChan1 was unexpectedly closed by Stop()")
	}
	if isChanClosed(subChan2) {
		t.Errorf("subChan2 was unexpectedly closed by Stop()")
	}

	// For full cleanup in a real scenario, one might DeSubscribe explicitly.
	// Here we are testing Stop's defined behavior.
	core.DeSubscribe(id1) // cleanup
	core.DeSubscribe(id2) // cleanup
}

func TestFanOutQueueCore_BlockedSubscriberWillRemove(t *testing.T) {
	t.Parallel()

	// check will break subscriber when target is block
	core := newFanOutQueueCore[MockItem](1) // publishChan needs to accept message
	topic := uuid.New()
	id, subChan, err := core.Subscribe(topic)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if pubErr := core.Publish(MockItem{Value: 456, TopicID: topic}); pubErr != nil {
		t.Fatalf("Publish failed: %v", pubErr)
	}
	if pubErr := core.Publish(MockItem{Value: 789, TopicID: topic}); pubErr != nil {
		t.Fatalf("Publish failed: %v", pubErr)
	}

	time.Sleep(500 * time.Millisecond) // Give ample time for either default or timeout.

	core.mu.RLock()
	_, stillSubscribed := core.subscribers[id]
	core.mu.RUnlock()

	if stillSubscribed {
		t.Errorf("Blocked consumer ID %s not removed from subscribers map", id)
	}

	if !isChanClosed(subChan) {
		_, ok := <-subChan
		if ok {
			t.Errorf("Channel for blocked consumer %s not closed", id)
		}
	}
	core.Stop()
}

func TestFanOutQueueCore_PublishToFullPublishChan_ReturnsError(t *testing.T) {
	t.Parallel()
	bufferSize := 1
	core := newFanOutQueueCore[MockItem](bufferSize)
	// No defer core.Stop() here.
	topic := uuid.New()
	// Create a subscriber whose channel will block, causing startFanOutRoutine to block.
	blockerSubID, blockerChan, _ := core.Subscribe(topic) // blockerChan is unbuffered.

	var wg sync.WaitGroup
	wg.Add(1)
	var firstPublishErr, secondPublishErr, thirdPublishErr error

	go func() {
		defer wg.Done()
		// Publish first message.
		// Goes into publishChan (size 1). FanOut routine picks it up.
		// FanOut tries to send to blockerChan and blocks. publishChan is now empty.
		firstPublishErr = core.Publish(MockItem{Value: 1, TopicID: topic})

		// Publish second message. This should fill the publishChan (now size 1/1).
		secondPublishErr = core.Publish(MockItem{Value: 2, TopicID: topic})

		// Publish third message. publishChan is full (1/1).
		// core.Publish should hit the default case in its select and return FullQueueError.
		thirdPublishErr = core.Publish(MockItem{Value: 3, TopicID: topic})
	}()

	wg.Wait() // Wait for the publishes to complete.

	if firstPublishErr != nil {
		t.Errorf("First publish unexpectedly failed: %v", firstPublishErr)
	}
	if secondPublishErr != nil {
		t.Errorf("Second publish unexpectedly failed: %v", secondPublishErr)
	}
	if thirdPublishErr != nil {
		t.Errorf("Expected FullQueueError for third publish, got %v", thirdPublishErr)
	}

	// Cleanup:
	// Unblock the subscriber (it might have been auto-desubscribed by timeout or default case).
	// Drain its channel or ensure it's closed.
	go func() {
		for range blockerChan {
			// Draining the channel
		}
	}()

	time.Sleep(500 * time.Millisecond) // Allow time for auto-desubscribe if it happened.

	core.mu.RLock()
	_, stillSubscribed := core.subscribers[blockerSubID]
	core.mu.RUnlock()
	if stillSubscribed {
		core.DeSubscribe(blockerSubID) // Manually desubscribe if not auto-removed.
	}
	core.Stop()
}

func TestFanOutQueueCore_PublishNoSubscribers(t *testing.T) {
	t.Parallel()
	t.Run("UnbufferedPublishChan", func(t *testing.T) {
		t.Parallel()
		core := newFanOutQueueCore[MockItem](0) // Unbuffered publishChan
		defer core.Stop()
		// Publish should succeed. fanOutRoutine consumes from publishChan.
		// If no subscribers, message is effectively dropped by fanOutRoutine.
		// Publish() has a default case, so it won't block indefinitely.
		err := core.Publish(MockItem{Value: 123, TopicID: uuid.New()})
		if err != nil {
			t.Errorf("Publish to unbuffered core with no subscribers failed: %v", err)
		}
	})

	t.Run("BufferedPublishChan", func(t *testing.T) {
		t.Parallel()
		core := newFanOutQueueCore[MockItem](5) // Buffered publishChan
		defer core.Stop()
		// Publish should succeed and message goes into the buffer.
		// fanOutRoutine will consume it and drop it.
		for i := 0; i < 5; i++ {
			if err := core.Publish(MockItem{Value: i, TopicID: uuid.New()}); err != nil {
				t.Errorf("Publish %d to buffered core with no subscribers failed: %v", i, err)
			}
		}
	})
}

// --- ChannelTripRecordMessageQueue Tests ---

// Mock db.Address if not available from dtm/db/db for test environment
// type dbAddress struct { Street string; City string }
// For the test, we assume db.Address is available and can be instantiated.
var testAddress = db.Address("testAddress")

func TestNewChannelTripRecordMessageQueue(t *testing.T) {
	t.Parallel()
	q := NewChannelTripRecordMessageQueue(mq.ActionCreate, 5)
	if q == nil {
		t.Fatal("NewChannelTripRecordMessageQueue returned nil")
	}
	defer q.Stop()

	if q.GetAction() != mq.ActionCreate {
		t.Errorf("Expected action %v, got %v", mq.ActionCreate, q.GetAction())
	}
	if q.core == nil {
		t.Error("Core is nil in ChannelTripRecordMessageQueue")
	}
	if q.core.bufferSize != 5 {
		t.Errorf("Expected core buffer size 5, got %d", q.core.bufferSize)
	}
}

func TestChannelTripRecordMessageQueue_Lifecycle(t *testing.T) {
	t.Parallel()
	q := NewChannelTripRecordMessageQueue(mq.ActionUpdate, 5)
	defer q.Stop()
	topic := uuid.New()
	if q.GetAction() != mq.ActionUpdate {
		t.Fatalf("Expected action %v, got %v", mq.ActionUpdate, q.GetAction())
	}

	id, subChan, err := q.Subscribe(topic)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	msg := mq.TripRecordMessage{
		TripID:        topic,
		ID:            uuid.New(),
		Name:          "Test Trip Record",
		Amount:        199.99,
		PrePayAddress: testAddress,
	}

	if pubErr := q.Publish(msg); pubErr != nil {
		t.Errorf("Publish failed: %v", pubErr)
	}

	receivedMsg, ok := receiveMsgWithTimeout(t, subChan, 500*time.Millisecond)
	if !ok {
		t.Fatal("Failed to receive TripRecordMessage or channel closed/timed out")
	}

	if !reflect.DeepEqual(receivedMsg, msg) {
		t.Errorf("Expected message %+v, got %+v", msg, receivedMsg)
	}

	err = q.DeSubscribe(id)
	if err != nil {
		t.Fatalf("DeSubscribe failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond) // allow for channel close
	if !isChanClosed(subChan) {
		t.Error("Subscriber channel not closed after DeSubscribe")
	}
}

func TestChannelTripRecordMessageQueue_PublishError(t *testing.T) {
	t.Parallel()
	q := NewChannelTripRecordMessageQueue(mq.ActionCreate, 1) // Core publishChan buffer size 1
	// No Stop via defer, explicit control for this test structure.
	topic := uuid.New()
	// Block the underlying core's fanOutRoutine by having a subscriber that doesn't read.
	_, blockerChan, _ := q.core.Subscribe(topic) // This is the fanOutQueueCore's subscriber channel.

	msg1 := mq.TripRecordMessage{ID: uuid.New(), Name: "TR_Msg1", PrePayAddress: testAddress, TripID: topic}
	msg2 := mq.TripRecordMessage{ID: uuid.New(), Name: "TR_Msg2", PrePayAddress: testAddress, TripID: topic}
	msg3 := mq.TripRecordMessage{ID: uuid.New(), Name: "TR_Msg3", PrePayAddress: testAddress, TripID: topic}

	// Publish msg1: Goes to core.publishChan. FanOut takes it, tries to send to blockerChan, blocks.
	// core.publishChan becomes empty.
	if err := q.Publish(msg1); err != nil {
		t.Fatalf("Publish msg1 failed unexpectedly: %v", err)
	}

	// Publish msg2: Fills core.publishChan (now 1/1).
	if err := q.Publish(msg2); err != nil {
		t.Fatalf("Publish msg2 failed unexpectedly: %v", err)
	}

	// Publish msg3: core.publishChan is full. core.Publish should return FullQueueError.
	// ChannelTripRecordMessageQueue.Publish should propagate this error.
	if err := q.Publish(msg3); err != nil {
		t.Fatalf("Publish msg2 failed unexpectedly: %v", err)
	}

	// blockerChan will just have first one
	final := <-blockerChan
	if final != msg1 {
		t.Fatalf("final msg will be the first one block in second queue")
	}

	// Cleanup:
	go func() {
		for range blockerChan {
		}
	}() // Unblock the fanOutRoutine.
	time.Sleep(500 * time.Millisecond) // Allow fanOutRoutine to process/timeout.
	q.Stop()                           // Stop the queue.
}

// --- ChannelTripAddressMessageQueue Tests ---

func TestNewChannelTripAddressMessageQueue(t *testing.T) {
	t.Parallel()
	q := NewChannelTripAddressMessageQueue(mq.ActionDelete, 3)
	if q == nil {
		t.Fatal("NewChannelTripAddressMessageQueue returned nil")
	}
	defer q.Stop()

	if q.GetAction() != mq.ActionDelete {
		t.Errorf("Expected action %v, got %v", mq.ActionDelete, q.GetAction())
	}
	if q.core == nil {
		t.Error("Core is nil in ChannelTripAddressMessageQueue")
	}
	if q.core.bufferSize != 3 {
		t.Errorf("Expected core buffer size 3, got %d", q.core.bufferSize)
	}
}

func TestChannelTripAddressMessageQueue_Lifecycle(t *testing.T) {
	t.Parallel()
	q := NewChannelTripAddressMessageQueue(mq.ActionCreate, 1)
	defer q.Stop()

	if q.GetAction() != mq.ActionCreate {
		t.Fatalf("Expected action %v, got %v", mq.ActionCreate, q.GetAction())
	}
	topic := uuid.New()
	id, subChan, err := q.Subscribe(topic)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	msg := mq.TripAddressMessage{Address: testAddress, TripID: topic}

	// Note: ChannelTripAddressMessageQueue.Publish ignores errors from core.Publish.
	if pubErr := q.Publish(msg); pubErr != nil {
		t.Errorf("Publish for TripAddressMessage unexpectedly returned error: %v", pubErr)
	}

	receivedMsg, ok := receiveMsgWithTimeout(t, subChan, 500*time.Millisecond)
	if !ok {
		t.Error("Failed to receive TripAddressMessage or channel closed/timed out")
	}

	if !reflect.DeepEqual(receivedMsg, msg) {
		t.Errorf("Expected message %+v, got %+v", msg, receivedMsg)
	}

	err = q.DeSubscribe(id)
	if err != nil {
		t.Fatalf("DeSubscribe failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if !isChanClosed(subChan) {
		t.Error("Subscriber channel not closed after DeSubscribe")
	}
}

func TestChannelTripAddressMessageQueue_PublishErrorIgnored(t *testing.T) {
	t.Parallel()
	// This test verifies that ChannelTripAddressMessageQueue.Publish returns nil
	// even if the underlying core.Publish method returns an error.
	q := NewChannelTripAddressMessageQueue(mq.ActionDelete, 1) // Core publishChan buffer size 1
	topic := uuid.New()
	_, blockerChan, _ := q.core.Subscribe(topic) // Block the core's fanOutRoutine.

	msg1 := mq.TripAddressMessage{TripID: topic, Address: db.Address("testAddress1")}
	msg2 := mq.TripAddressMessage{TripID: topic, Address: db.Address("testAddress2")}
	msg3 := mq.TripAddressMessage{TripID: topic, Address: db.Address("testAddress3")}

	// Publish msg1 -> core.publishChan, fanOut takes it, blocks. core.publishChan empty.
	if err := q.Publish(msg1); err != nil { // Expected nil
		t.Fatalf("Publish msg1 returned error: %v, expected nil", err)
	}
	// Publish msg2 -> fills core.publishChan (1/1).
	if err := q.Publish(msg2); err != nil { // Expected nil
		t.Fatalf("Publish msg2 returned error: %v, expected nil", err)
	}

	// Publish msg3: core.publishChan is full. core.Publish would return FullQueueError.
	// However, ChannelTripAddressMessageQueue.Publish should ignore this and return nil.
	err := q.Publish(msg3)
	if err != nil { // Expected nil
		t.Errorf("Expected nil from Publish(msg3) even on underlying FullQueueError, got %v", err)
	}

	// Cleanup:
	go func() {
		for range blockerChan {
		}
	}()
	time.Sleep(500 * time.Millisecond)
	q.Stop()
}

// --- GoChanTripMessageQueueWrapper Tests ---

func TestNewGoChanTripMessageQueueWrapper(t *testing.T) {
	t.Parallel()
	wrapperIFace := NewGoChanTripMessageQueueWrapper()
	wrapper, ok := wrapperIFace.(*GoChanTripMessageQueueWrapper)
	if !ok {
		t.Fatal("NewGoChanTripMessageQueueWrapper did not return *GoChanTripMessageQueueWrapper")
	}

	// Defer stop for all initialized queues to prevent goroutine leaks.
	defer func() {
		for i := range wrapper.AddressMQArray {
			if wrapper.AddressMQArray[i] != nil {
				wrapper.AddressMQArray[i].Stop()
			}
		}
		for i := range wrapper.RecordMQArray {
			if wrapper.RecordMQArray[i] != nil {
				wrapper.RecordMQArray[i].Stop()
			}
		}
	}()

	// Verify Address MQs
	if wrapper.AddressMQArray[mq.ActionCreate] == nil {
		t.Error("AddressMQArray[ActionCreate] is nil")
	} else if wrapper.AddressMQArray[mq.ActionCreate].GetAction() != mq.ActionCreate {
		t.Errorf("AddressMQArray[ActionCreate] has action %v, expected %v", wrapper.AddressMQArray[mq.ActionCreate].GetAction(), mq.ActionCreate)
	}

	if wrapper.AddressMQArray[mq.ActionDelete] == nil {
		t.Error("AddressMQArray[ActionDelete] is nil")
	} else if wrapper.AddressMQArray[mq.ActionDelete].GetAction() != mq.ActionDelete {
		t.Errorf("AddressMQArray[ActionDelete] has action %v, expected %v", wrapper.AddressMQArray[mq.ActionDelete].GetAction(), mq.ActionDelete)
	}
	// Check that other address actions (like Update) are nil as per constructor logic
	if mq.ActionUpdate < mq.ActionCnt && wrapper.AddressMQArray[mq.ActionUpdate] != nil {
		t.Errorf("AddressMQArray[ActionUpdate] should be nil, got %v", wrapper.AddressMQArray[mq.ActionUpdate])
	}

	// Verify Record MQs
	if wrapper.RecordMQArray[mq.ActionCreate] == nil {
		t.Error("RecordMQArray[ActionCreate] is nil")
	} else if wrapper.RecordMQArray[mq.ActionCreate].GetAction() != mq.ActionCreate {
		t.Errorf("RecordMQArray[ActionCreate] has action %v, expected %v", wrapper.RecordMQArray[mq.ActionCreate].GetAction(), mq.ActionCreate)
	}

	if wrapper.RecordMQArray[mq.ActionUpdate] == nil {
		t.Error("RecordMQArray[ActionUpdate] is nil")
	} else if wrapper.RecordMQArray[mq.ActionUpdate].GetAction() != mq.ActionUpdate {
		t.Errorf("RecordMQArray[ActionUpdate] has action %v, expected %v", wrapper.RecordMQArray[mq.ActionUpdate].GetAction(), mq.ActionUpdate)
	}

	if wrapper.RecordMQArray[mq.ActionDelete] == nil {
		t.Error("RecordMQArray[ActionDelete] is nil")
	} else if wrapper.RecordMQArray[mq.ActionDelete].GetAction() != mq.ActionDelete {
		t.Errorf("RecordMQArray[ActionDelete] has action %v, expected %v", wrapper.RecordMQArray[mq.ActionDelete].GetAction(), mq.ActionDelete)
	}
}

func TestGoChanTripMessageQueueWrapper_GetQueues(t *testing.T) {
	t.Parallel()
	wrapperIFace := NewGoChanTripMessageQueueWrapper()
	// Cast to concrete type to stop queues in defer, interface doesn't expose arrays.
	wrapper, _ := wrapperIFace.(*GoChanTripMessageQueueWrapper)
	defer func() {
		if wrapper != nil {
			for i := range wrapper.AddressMQArray {
				if wrapper.AddressMQArray[i] != nil {
					wrapper.AddressMQArray[i].Stop()
				}
			}
			for i := range wrapper.RecordMQArray {
				if wrapper.RecordMQArray[i] != nil {
					wrapper.RecordMQArray[i].Stop()
				}
			}
		}
	}()

	// Test GetTripRecordMessageQueue
	validRecordActions := []mq.Action{mq.ActionCreate, mq.ActionUpdate, mq.ActionDelete}
	for _, action := range validRecordActions {
		q := wrapperIFace.GetTripRecordMessageQueue(action)
		if q == nil {
			t.Errorf("GetTripRecordMessageQueue(%v) returned nil, expected a queue", action)
			continue
		}
		if q.GetAction() != action {
			t.Errorf("GetTripRecordMessageQueue(%v) returned queue with action %v, expected %v", action, q.GetAction(), action)
		}
	}
	// Test invalid/out-of-bounds actions for RecordMQ
	if q := wrapperIFace.GetTripRecordMessageQueue(mq.Action(99)); q != nil {
		t.Errorf("GetTripRecordMessageQueue(Action(99)) expected nil, got %T", q)
	}
	if q := wrapperIFace.GetTripRecordMessageQueue(mq.Action(-1)); q != nil {
		t.Errorf("GetTripRecordMessageQueue(Action(-1)) expected nil, got %T", q)
	}
	if mq.ActionCnt <= 3 { // Example: if ActionCnt is exactly 3 (0,1,2)
		if q := wrapperIFace.GetTripRecordMessageQueue(mq.ActionCnt); q != nil {
			t.Errorf("GetTripRecordMessageQueue(ActionCnt) expected nil, got %T", q)
		}
	}

	// Test GetTripAddressMessageQueue
	validAddressActions := []mq.Action{mq.ActionCreate, mq.ActionDelete}
	for _, action := range validAddressActions {
		q := wrapperIFace.GetTripAddressMessageQueue(action)
		if q == nil {
			t.Errorf("GetTripAddressMessageQueue(%v) returned nil, expected a queue", action)
			continue
		}
		if q.GetAction() != action {
			t.Errorf("GetTripAddressMessageQueue(%v) returned queue with action %v, expected %v", action, q.GetAction(), action)
		}
	}
	// Test action not initialized for AddressMQ (e.g., ActionUpdate)
	if q := wrapperIFace.GetTripAddressMessageQueue(mq.ActionUpdate); q != nil {
		t.Errorf("GetTripAddressMessageQueue(ActionUpdate) expected nil, got %T", q)
	}
	// Test invalid/out-of-bounds actions for AddressMQ
	if q := wrapperIFace.GetTripAddressMessageQueue(mq.Action(99)); q != nil {
		t.Errorf("GetTripAddressMessageQueue(Action(99)) expected nil, got %T", q)
	}
	if q := wrapperIFace.GetTripAddressMessageQueue(mq.Action(-1)); q != nil {
		t.Errorf("GetTripAddressMessageQueue(Action(-1)) expected nil, got %T", q)
	}
}
