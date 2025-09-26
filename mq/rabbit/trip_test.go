// mq_interface_rabbit_test.go
package rabbit_test // Testing the 'rabbit' package as a black box providing 'mq' interfaces

import (

	// "MODULE_PATH/YOUR_PROJECT/dtm/db/db" // Assuming this path for db.Address
	"dtm/db/db"
	"dtm/mq/mq"              // MQ interfaces
	rabbitMQ "dtm/mq/rabbit" // RabbitMQ implementation of MQ interfaces
	"fmt"
	"log"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// --- Test Helper Functions ---

// getTestConnection establishes a real AMQP connection for tests.
// It fails the test if a connection cannot be established.
func getTestConnection(t *testing.T) *amqp.Connection {
	t.Helper()
	url := rabbitMQ.CreateAmqpURL()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("PRE-REQUISITE FAILED: Could not connect to RabbitMQ at %s for testing. Ensure it's running and accessible. Error: %v", url, err)
	}
	return conn
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

var testAddressValue = db.Address("123 Test St")

// --- Test Suite ---

func TestMQInterfacesWithRabbitMQ(t *testing.T) {
	conn := getTestConnection(t)
	defer func(conn *amqp.Connection) {
		err := conn.Close()
		if err != nil {
			log.Fatalf("Error closing connection: %v", err)
		}
	}(conn)

	wrapper, err := rabbitMQ.NewRabbitTripMessageQueueWrapper(conn)
	if err != nil {
		t.Fatalf("Failed to create RabbitTripMessageQueueWrapper: %v", err)
	}

	// --- Test TopicProvider (implicitly via message structs) ---
	t.Run("TopicProvider", func(t *testing.T) {
		tripID := uuid.New()
		trMsg := mq.TripRecordMessage{TripID: tripID}
		taMsg := mq.TripAddressMessage{TripID: tripID}

		if trMsg.GetTopic() != tripID {
			t.Errorf("TripRecordMessage.GetTopic() expected %s, got %s", tripID, trMsg.GetTopic())
		}
		if taMsg.GetTopic() != tripID {
			t.Errorf("TripAddressMessage.GetTopic() expected %s, got %s", tripID, taMsg.GetTopic())
		}
	})

	// --- Test TripRecordMessageQueue ---
	t.Run("TripRecordMessageQueue", func(t *testing.T) {
		testAction := mq.ActionCreate
		trq := wrapper.GetTripRecordMessageQueue(testAction)
		if trq == nil {
			t.Fatalf("GetTripRecordMessageQueue(%v) returned nil", testAction)
		}

		// Test GetAction
		if trq.GetAction() != testAction {
			t.Errorf("TripRecordMessageQueue.GetAction() expected %v, got %v", testAction, trq.GetAction())
		}

		// Test Lifecycle (Publish, Subscribe, Receive, DeSubscribe)
		t.Run("Lifecycle_SingleSub", func(t *testing.T) {
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

			// Allow consumer to start
			time.Sleep(200 * time.Millisecond)

			if err := trq.Publish(msgToPublish); err != nil {
				t.Fatalf("trq.Publish failed: %v", err)
			}

			receivedMsg, ok := receiveMsgWithTimeout(t, rcvChan, 3*time.Second)
			if !ok {
				t.Fatalf("Timeout or channel closed while waiting for message on TripRecordMessageQueue")
			}
			if !reflect.DeepEqual(receivedMsg, msgToPublish) {
				t.Errorf("Received TR message\n%+v\ndoes not match published message\n%+v", receivedMsg, msgToPublish)
			}

			if err := trq.DeSubscribe(subID); err != nil {
				t.Fatalf("trq.DeSubscribe failed: %v", err)
			}

			// Check channel is closed
			time.Sleep(200 * time.Millisecond) // Give time for async close
			if !isChanClosed(rcvChan) {
				// Try one more read to confirm
				_, stillOpen := <-rcvChan
				if stillOpen {
					t.Error("TR subscriber channel not closed after DeSubscribe")
				}
			}
		})

		t.Run("MultipleSubscribers_SameTopic", func(t *testing.T) {
			topicID := uuid.New()
			msgToPublish := mq.TripRecordMessage{ID: uuid.New(), TripID: topicID, Name: "TR Multi-Sub Test"}

			subID1, rcvChan1, _ := trq.Subscribe(topicID)
			subID2, rcvChan2, _ := trq.Subscribe(topicID)
			defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
				err := trq.DeSubscribe(id)
				if err != nil {
					log.Fatalf("trq.DeSubscribe failed: %v", err)
				}
			}(trq, subID1) // Ensure cleanup
			defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
				err := trq.DeSubscribe(id)
				if err != nil {
					log.Fatalf("trq.DeSubscribe failed: %v", err)
				}
			}(trq, subID2) // Ensure cleanup

			time.Sleep(200 * time.Millisecond)
			if err := trq.Publish(msgToPublish); err != nil {
				t.Fatalf("Publish failed: %v", err)
			}

			for i, ch := range []<-chan mq.TripRecordMessage{rcvChan1, rcvChan2} {
				receivedMsg, ok := receiveMsgWithTimeout(t, ch, 1*time.Second)
				if !ok {
					t.Errorf("Subscriber %d timed out or channel closed", i+1)
					continue
				}
				if !reflect.DeepEqual(receivedMsg, msgToPublish) {
					t.Errorf("Subscriber %d received %+v, expected %+v", i+1, receivedMsg, msgToPublish)
				}
			}
		})

		t.Run("Subscribers_DifferentTopics", func(t *testing.T) {
			topicA := uuid.New()
			topicB := uuid.New()
			msgA := mq.TripRecordMessage{ID: uuid.New(), TripID: topicA, Name: "Message A"}
			msgB := mq.TripRecordMessage{ID: uuid.New(), TripID: topicB, Name: "Message B"}

			subA, rcvA, _ := trq.Subscribe(topicA)
			subB, rcvB, _ := trq.Subscribe(topicB)
			defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
				err := trq.DeSubscribe(id)
				if err != nil {
					log.Fatalf("trq.DeSubscribe failed: %v", err)
				}
			}(trq, subA)
			defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
				err := trq.DeSubscribe(id)
				if err != nil {
					log.Fatalf("trq.DeSubscribe failed: %v", err)
				}
			}(trq, subB)

			time.Sleep(200 * time.Millisecond)
			if err := trq.Publish(msgA); err != nil {
				t.Fatalf("Publish msgA failed: %v", err)
			}
			if err := trq.Publish(msgB); err != nil {
				t.Fatalf("Publish msgB failed: %v", err)
			}

			// Check A receives msgA
			recA, okA := receiveMsgWithTimeout(t, rcvA, 3*time.Second)
			if !okA || !reflect.DeepEqual(recA, msgA) {
				t.Errorf("Sub A: expected %+v, got %+v (ok: %t)", msgA, recA, okA)
			}
			// Check B receives msgB
			recB, okB := receiveMsgWithTimeout(t, rcvB, 3*time.Second)
			if !okB || !reflect.DeepEqual(recB, msgB) {
				t.Errorf("Sub B: expected %+v, got %+v (ok: %t)", msgB, recB, okB)
			}

			// Check A does not receive msgB (its channel should be empty or contain only msgA)
			_, okA2 := receiveMsgWithTimeout(t, rcvA, 50*time.Millisecond) // Short timeout
			if okA2 {
				t.Error("Sub A received an unexpected second message (should have been for Topic B)")
			}
			// Check B does not receive msgA
			_, okB2 := receiveMsgWithTimeout(t, rcvB, 50*time.Millisecond) // Short timeout
			if okB2 {
				t.Error("Sub B received an unexpected second message (should have been for Topic A)")
			}
		})

		t.Run("DeSubscribe_NonExistent", func(t *testing.T) {
			err := trq.DeSubscribe(uuid.New())
			if err == nil {
				t.Error("Expected error when de-subscribing non-existent ID from TRQ, got nil")
			}
		})

		t.Run("Receive_2_Messages_BySameSubscription", func(t *testing.T) {
			topicID := uuid.New()
			msg1 := mq.TripRecordMessage{ID: uuid.New(), TripID: topicID, Name: "TR Multi-Receive 1"}
			msg2 := mq.TripRecordMessage{ID: uuid.New(), TripID: topicID, Name: "TR Multi-Receive 2"}

			subID, rcvChan, err := trq.Subscribe(topicID)
			if err != nil {
				t.Fatalf("Subscribe failed: %v", err)
			}
			defer func(trq mq.TripRecordMessageQueue, id uuid.UUID) {
				err := trq.DeSubscribe(id)
				if err != nil {
					log.Fatalf("trq.DeSubscribe failed: %v", err)
				}
			}(trq, subID)

			time.Sleep(200 * time.Millisecond) // Allow consumer to start
			if err := trq.Publish(msg1); err != nil {
				t.Fatalf("Publish msg1 failed: %v", err)
			}
			if err := trq.Publish(msg2); err != nil {
				t.Fatalf("Publish msg2 failed: %v", err)
			}

			receivedMsg1, ok1 := receiveMsgWithTimeout(t, rcvChan, 3*time.Second)
			if !ok1 {
				t.Fatal("Timeout or channel closed while waiting for first message on TripRecordMessageQueue")
			}
			if !reflect.DeepEqual(receivedMsg1, msg1) {
				t.Errorf("Received first TR message\n%+v\ndoes not match published message\n%+v", receivedMsg1, msg1)
			}
			receivedMsg2, ok2 := receiveMsgWithTimeout(t, rcvChan, 3*time.Second)
			if !ok2 {
				t.Fatal("Timeout or channel closed while waiting for second message on TripRecordMessageQueue")
			}
			if !reflect.DeepEqual(receivedMsg2, msg2) {
				t.Errorf("Received second TR message\n%+v\ndoes not match published message\n%+v", receivedMsg2, msg2)
			}
			// Ensure no more messages are left in the channel
			_, ok3 := receiveMsgWithTimeout(t, rcvChan, 100*time.Millisecond) // Short timeout
			if ok3 {
				t.Error("Received unexpected third message on TripRecordMessageQueue after two publishes")
			}
		})

		t.Run("Publish_NoSubscribers", func(t *testing.T) {
			topicID := uuid.New()
			msg := mq.TripRecordMessage{ID: uuid.New(), TripID: topicID, Name: "TR No-Sub Test"}
			err := trq.Publish(msg) // Should not error, message just won't be picked up by a test subscriber
			if err != nil {
				t.Errorf("Publish to TRQ with no subscribers failed: %v", err)
			}
		})
	})

	// --- Test TripAddressMessageQueue ---
	t.Run("TripAddressMessageQueue", func(t *testing.T) {
		testAction := mq.ActionDelete // Example action for address queue
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

			receivedMsg, ok := receiveMsgWithTimeout(t, rcvChan, 3*time.Second)
			if !ok {
				t.Fatalf("Timeout or channel closed while waiting for message on TripAddressMessageQueue")
			}
			if !reflect.DeepEqual(receivedMsg, msgToPublish) {
				t.Errorf("Received TA message\n%+v\ndoes not match published message\n%+v", receivedMsg, msgToPublish)
			}
		})
		// Add more tests for TripAddressMessageQueue similar to TripRecordMessageQueue (multi-sub, diff topics, etc.)
		// Be mindful of any specific error handling behavior in its Publish method.
	})

	// --- Test TripMessageQueueWrapper ---
	t.Run("TripMessageQueueWrapper_Getters", func(t *testing.T) {
		// Test GetTripRecordMessageQueue
		validRecordActions := []mq.Action{mq.ActionCreate, mq.ActionUpdate, mq.ActionDelete}
		for _, action := range validRecordActions {
			q := wrapper.GetTripRecordMessageQueue(action)
			if q == nil {
				t.Errorf("Wrapper.GetTripRecordMessageQueue(%v) returned nil", action)
				continue
			}
			if q.GetAction() != action {
				t.Errorf("Wrapper.GetTripRecordMessageQueue(%v) returned queue with action %v", action, q.GetAction())
			}
		}
		if q := wrapper.GetTripRecordMessageQueue(mq.Action(99)); q != nil {
			t.Errorf("Wrapper.GetTripRecordMessageQueue(invalid) expected nil, got %T", q)
		}

		// Test GetTripAddressMessageQueue
		validAddressActions := []mq.Action{mq.ActionCreate, mq.ActionDelete}
		for _, action := range validAddressActions {
			q := wrapper.GetTripAddressMessageQueue(action)
			if q == nil {
				t.Errorf("Wrapper.GetTripAddressMessageQueue(%v) returned nil", action)
				continue
			}
			if q.GetAction() != action {
				t.Errorf("Wrapper.GetTripAddressMessageQueue(%v) returned queue with action %v", action, q.GetAction())
			}
		}
		// Test ActionUpdate for AddressMQ, which is configured to be nil in the rabbit implementation
		if q := wrapper.GetTripAddressMessageQueue(mq.ActionUpdate); q != nil {
			t.Errorf("Wrapper.GetTripAddressMessageQueue(ActionUpdate) expected nil, got %T", q)
		}
		if q := wrapper.GetTripAddressMessageQueue(mq.Action(99)); q != nil {
			t.Errorf("Wrapper.GetTripAddressMessageQueue(invalid) expected nil, got %T", q)
		}
	})

	// Context for cancellation test
	t.Run("ContextCancellationDuringSubscriptionProcessing", func(t *testing.T) {
		trq := wrapper.GetTripRecordMessageQueue(mq.ActionUpdate)
		if trq == nil {
			t.Fatal("Failed to get TripRecordMessageQueue for context cancellation test")
		}

		topicID := uuid.New()
		msgToPublish := mq.TripRecordMessage{ID: uuid.New(), TripID: topicID, Name: "Context Cancel Test"}

		// Use the SubscribeProcessor from the 'utils.go' file (assuming it's part of the 'rabbit' package or accessible)
		// For this test, we need a transformFunc and outputStream.
		// The original 'utils.go' SubscribeProcessor is generic.
		// Your 'rabbit' package doesn't directly expose a 'SubscribeProcessor',
		// it's used internally by services that implement Subscribe.
		// The test here will focus on the behavior of the Subscribe method of the queue itself.
		// If a context is implicitly used by the Subscribe/goroutine logic in the rabbit implementation,
		// testing its cancellation effect directly on the interface might be hard without specific design for it.
		// The rabbit.GenericRabbitMQService's Subscribe goroutine DOES listen to a stopChan (from DeSubscribe)
		// and also handles delivery channel closure. It doesn't directly take a context for its main loop,
		// but the PublishWithContext uses a context.

		// Let's test if DeSubscribe effectively stops message processing quickly.
		subID, rcvChan, err := trq.Subscribe(topicID)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		// Publish a message
		if err := trq.Publish(msgToPublish); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}

		// Receive the first message
		_, ok := receiveMsgWithTimeout(t, rcvChan, 3*time.Second)
		if !ok {
			t.Fatal("Failed to receive the first message")
		}

		// DeSubscribe
		if err := trq.DeSubscribe(subID); err != nil {
			t.Fatalf("DeSubscribe failed: %v", err)
		}

		// Try publishing more messages. They should not be received as the subscription is gone
		// and the internal goroutine for that subscription should have exited.
		for i := 0; i < 3; i++ {
			if err := trq.Publish(mq.TripRecordMessage{ID: uuid.New(), TripID: topicID, Name: fmt.Sprintf("Post-DeSub %d", i)}); err != nil {
				t.Logf("Publish after de-subscribe failed (this might be ok if channel closed): %v", err)
				// Depending on implementation, publish might fail if no routes after all subs gone, or just be dropped.
			}
		}

		// The channel should be closed and empty
		time.Sleep(200 * time.Millisecond) // ensure close propagates
		msg, ok := receiveMsgWithTimeout(t, rcvChan, 100*time.Millisecond)
		if ok {
			t.Errorf("Received unexpected message %+v after DeSubscribe", msg)
		}
		if !isChanClosed(rcvChan) {
			// One final check
			_, open := <-rcvChan
			if open {
				t.Error("Channel remains open after DeSubscribe and timeout")
			}
		}
		t.Log("Context/Cancellation test (via DeSubscribe) completed.")
	})
}
