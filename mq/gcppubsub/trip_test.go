package gcppubsub_test

import (
	"context"
	"dtm/db/db"
	"dtm/mq/gcppubsub" // Import the package to be tested
	"dtm/mq/mq"
	"log"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- Test Pre-requisite ---
// This test suite requires the Google Cloud Pub/Sub emulator to be running.
// Before running the tests, start the emulator using the gcloud CLI:
//
//	gcloud beta emulators pubsub start --project=test-project
//
// The tests will automatically detect the PUBSUB_EMULATOR_HOST environment
// variable set by the emulator. If it's not set, all tests will be skipped.
// The project ID used here ("test-project") must match the one used to start the emulator.
const testProjectID = "test-project"

// --- Test Helper Functions ---

// getTestWrapper connects to the Pub/Sub emulator and creates a new wrapper for testing.
// It skips the test if the emulator is not running.
func getTestWrapper(t *testing.T) mq.TripMessageQueueWrapper {
	t.Helper()
	if os.Getenv("PUBSUB_EMULATOR_HOST") == "" {
		t.Skip("Skipping test: PUBSUB_EMULATOR_HOST environment variable not set. Please start the Pub/Sub emulator.")
	}
	// os.Setenv("GCP_PROJECT_ID", "gcp-exercise-434714")

	ctx := context.Background()
	wrapper, err := gcppubsub.NewGCPTripMessageQueueWrapper(ctx, testProjectID)
	if err != nil {
		t.Fatalf("Failed to create GCPTripMessageQueueWrapper for emulator: %v", err)
	}
	return wrapper
}

// receiveMsgWithTimeout attempts to receive a message from a channel with a specified timeout.
// Returns the message and true if successful, or the zero value of T and false on timeout or if the channel is closed.
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

// isChanClosed checks if a channel is closed in a non-blocking way.
// Returns true if the channel is closed, false otherwise.
func isChanClosed[T any](ch <-chan T) bool {
	select {
	case _, ok := <-ch:
		return !ok // ok is false if channel is closed
	default:
		return false // channel is open and would block or is empty
	}
}

// setupTripRecordQueue is a helper to reduce boilerplate in each test.
func setupTripRecordQueue(t *testing.T, action mq.Action) mq.TripRecordMessageQueue {
	t.Helper()
	// Assuming getTestWrapper() is available in your test file, which sets up the connection.
	wrapper := getTestWrapper(t)
	trq := wrapper.GetTripRecordMessageQueue(action)
	if trq == nil {
		t.Fatalf("GetTripRecordMessageQueue(%v) returned nil", action)
	}
	return trq
}

var testAddressValue = db.Address("123 Test St")

// --- Test Suite ---

func TestMQInterfacesWithGCPPubSub(t *testing.T) {
	wrapper := getTestWrapper(t)

	// --- Test TripAddressMessageQueue ---
	t.Run("TripAddressMessageQueue", func(t *testing.T) {
		testAction := mq.ActionDelete
		taq := wrapper.GetTripAddressMessageQueue(testAction)
		if taq == nil {
			t.Fatalf("GetTripAddressMessageQueue(%v) returned nil", testAction)
		}

		if taq.GetAction() != testAction {
			t.Errorf("TripAddressMessageQueue.GetAction() expected %v, got %v", testAction, taq.GetAction())
		}

		t.Run("Lifecycle_SingleSub", func(t *testing.T) {
			topicID := uuid.New()
			msgToPublish := mq.TripAddressMessage{
				TripID:  topicID,
				Address: testAddressValue,
			}

			subID, rcvChan, err := taq.Subscribe(topicID)
			if err != nil {
				t.Fatalf("taq.Subscribe failed: %v", err)
			}
			defer func(taq mq.TripAddressMessageQueue, id uuid.UUID) {
				err := taq.DeSubscribe(id)
				if err != nil {
					log.Fatalf("taq.DeSubscribe failed: %v", err)
				}
			}(taq, subID)

			time.Sleep(200 * time.Millisecond)
			if err := taq.Publish(msgToPublish); err != nil {
				t.Fatalf("taq.Publish failed: %v", err)
			}

			receivedMsg, ok := receiveMsgWithTimeout(t, rcvChan, 30*time.Second)
			if !ok {
				t.Fatalf("Timeout or channel closed while waiting for message on TripAddressMessageQueue")
			}
			if !reflect.DeepEqual(receivedMsg, msgToPublish) {
				t.Errorf("Received TA message\n%+v\ndoes not match published message\n%+v", receivedMsg, msgToPublish)
			}
		})
	})

	// --- Test TripMessageQueueWrapper ---
	t.Run("TripMessageQueueWrapper_Getters", func(t *testing.T) {
		// Test GetTripRecordMessageQueue
		validRecordActions := []mq.Action{mq.ActionCreate, mq.ActionUpdate, mq.ActionDelete}
		for _, action := range validRecordActions {
			q := wrapper.GetTripRecordMessageQueue(action)
			if q == nil {
				t.Errorf("Wrapper.GetTripRecordMessageQueue(%v) returned nil", action)
			}
		}

		// Test GetTripAddressMessageQueue
		validAddressActions := []mq.Action{mq.ActionCreate, mq.ActionDelete}
		for _, action := range validAddressActions {
			q := wrapper.GetTripAddressMessageQueue(action)
			if q == nil {
				t.Errorf("Wrapper.GetTripAddressMessageQueue(%v) returned nil", action)
			}
		}
		// Test ActionUpdate for AddressMQ, which is configured to be nil
		if q := wrapper.GetTripAddressMessageQueue(mq.ActionUpdate); q != nil {
			t.Errorf("Wrapper.GetTripAddressMessageQueue(ActionUpdate) expected nil, got %T", q)
		}
	})
}

func TestTripRecordMessageQueue_GetAction(t *testing.T) {
	t.Parallel() // Allows this test to run in parallel with others.
	testAction := mq.ActionCreate
	trq := setupTripRecordQueue(t, testAction)

	if trq.GetAction() != testAction {
		t.Errorf("TripRecordMessageQueue.GetAction() expected %v, got %v", testAction, trq.GetAction())
	}
}

func TestTripRecordMessageQueue_Lifecycle_SingleSub(t *testing.T) {
	t.Parallel()
	trq := setupTripRecordQueue(t, mq.ActionCreate)
	topicID := uuid.New()
	msgToPublish := mq.TripRecordMessage{
		ID:            uuid.New(),
		TripID:        topicID,
		Name:          "TR Lifecycle Test",
		Amount:        100.50,
		PrePayAddress: testAddressValue,
	}

	subID, rcvChan, err := trq.Subscribe(topicID)
	if err != nil {
		t.Fatalf("trq.Subscribe failed: %v", err)
	}
	if subID == uuid.Nil {
		t.Fatal("Subscribe returned nil ID")
	}
	// Allow time for subscription to be ready on the emulator backend
	time.Sleep(2 * time.Second)

	if err := trq.Publish(msgToPublish); err != nil {
		t.Fatalf("trq.Publish failed: %v", err)
	}

	receivedMsg, ok := receiveMsgWithTimeout(t, rcvChan, 30*time.Second)
	if !ok {
		t.Fatal("Timeout or channel closed while waiting for message on TripRecordMessageQueue")
	}
	if !reflect.DeepEqual(receivedMsg, msgToPublish) {
		t.Errorf("Received TR message\n%+v\ndoes not match published message\n%+v", receivedMsg, msgToPublish)
	}

	if err := trq.DeSubscribe(subID); err != nil {
		t.Fatalf("trq.DeSubscribe failed: %v", err)
	}

	// Check channel is closed
	time.Sleep(500 * time.Millisecond) // Give time for async close and subscription deletion
	if !isChanClosed(rcvChan) {
		_, stillOpen := <-rcvChan
		if stillOpen {
			t.Error("TR subscriber channel not closed after DeSubscribe")
		}
	}
}

func TestTripRecordMessageQueue_MultipleSubscribers_SameTopic(t *testing.T) {
	t.Parallel()
	trq := setupTripRecordQueue(t, mq.ActionCreate)
	topicID := uuid.New()
	msgToPublish := mq.TripRecordMessage{ID: uuid.New(), TripID: topicID, Name: "TR Multi-Sub Test"}

	subID1, rcvChan1, err := trq.Subscribe(topicID)
	if err != nil {
		t.Fatalf("trq.Subscribe failed for sub1: %v", err)
	}
	subID2, rcvChan2, err := trq.Subscribe(topicID)
	if err != nil {
		t.Fatalf("trq.Subscribe failed for sub2: %v", err)
	}
	defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
		err := trq.DeSubscribe(id)
		if err != nil {
			log.Fatalf("trq.DeSubscribe failed for sub1: %v", err)
		}
	}(trq, subID1) // Ensure cleanup
	defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
		err := trq.DeSubscribe(id)
		if err != nil {
			log.Fatalf("trq.DeSubscribe failed for sub2: %v", err)
		}
	}(trq, subID2) // Ensure cleanup

	time.Sleep(2 * time.Second)
	if err := trq.Publish(msgToPublish); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		receivedMsg, ok := receiveMsgWithTimeout(t, rcvChan1, 30*time.Second)
		if !ok {
			t.Errorf("Subscriber 1 timed out or channel closed")
			return
		}
		if !reflect.DeepEqual(receivedMsg, msgToPublish) {
			t.Errorf("Subscriber 1 received %+v, expected %+v", receivedMsg, msgToPublish)
		}
	}()

	go func() {
		defer wg.Done()
		receivedMsg, ok := receiveMsgWithTimeout(t, rcvChan2, 30*time.Second)
		if !ok {
			t.Errorf("Subscriber 2 timed out or channel closed")
			return
		}
		if !reflect.DeepEqual(receivedMsg, msgToPublish) {
			t.Errorf("Subscriber 2 received %+v, expected %+v", receivedMsg, msgToPublish)
		}
	}()

	wg.Wait()
}

func TestTripRecordMessageQueue_Subscribers_DifferentTopics_WithFilter(t *testing.T) {
	t.Parallel()
	trq := setupTripRecordQueue(t, mq.ActionCreate)
	topicA := uuid.New()
	topicB := uuid.New()
	msgA := mq.TripRecordMessage{ID: uuid.New(), TripID: topicA, Name: "Message A"}
	msgB := mq.TripRecordMessage{ID: uuid.New(), TripID: topicB, Name: "Message B"}

	subA, rcvA, err := trq.Subscribe(topicA)
	if err != nil {
		t.Fatalf("Subscribe for Topic A failed: %v", err)
	}
	subB, rcvB, err := trq.Subscribe(topicB)
	if err != nil {
		t.Fatalf("Subscribe for Topic B failed: %v", err)
	}
	defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
		err := trq.DeSubscribe(id)
		if err != nil {
			log.Printf("DeSubscribe failed: %v", err)
		}
	}(trq, subA)
	defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
		err := trq.DeSubscribe(id)
		if err != nil {
			log.Printf("DeSubscribe failed: %v", err)
		}
	}(trq, subB)

	time.Sleep(2 * time.Second)
	if err := trq.Publish(msgA); err != nil {
		t.Fatalf("Publish msgA failed: %v", err)
	}
	if err := trq.Publish(msgB); err != nil {
		t.Fatalf("Publish msgB failed: %v", err)
	}

	// Check A receives msgA
	recA, okA := receiveMsgWithTimeout(t, rcvA, 30*time.Second)
	if !okA || !reflect.DeepEqual(recA, msgA) {
		t.Errorf("Sub A: expected %+v, got %+v (ok: %t)", msgA, recA, okA)
	}
	// Check B receives msgB
	recB, okB := receiveMsgWithTimeout(t, rcvB, 30*time.Second)
	if !okB || !reflect.DeepEqual(recB, msgB) {
		t.Errorf("Sub B: expected %+v, got %+v (ok: %t)", msgB, recB, okB)
	}

	// Check A does not receive msgB (its channel should be empty)
	_, okA2 := receiveMsgWithTimeout(t, rcvA, 1*time.Second)
	if okA2 {
		t.Error("Sub A received an unexpected second message (should have been for Topic B)")
	}
	// Check B does not receive msgA
	_, okB2 := receiveMsgWithTimeout(t, rcvB, 1*time.Second)
	if okB2 {
		t.Error("Sub B received an unexpected second message (should have been for Topic A)")
	}
}

func TestTripRecordMessageQueue_DeSubscribe_NonExistent(t *testing.T) {
	t.Parallel()
	trq := setupTripRecordQueue(t, mq.ActionCreate)

	err := trq.DeSubscribe(uuid.New())
	if err == nil {
		t.Error("Expected error when de-subscribing non-existent ID from TRQ, got nil")
	}
}
