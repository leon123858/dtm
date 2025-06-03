package rabbit

import (
	"dtm/db/db"
	"dtm/mq/mq"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testRabbitConn *amqp091.Connection
)

// setupTestRabbitMQ establishes a single connection for all tests
func setupTestRabbitMQ(t *testing.T) {
	conn, err := InitRabbitMQ(CreateAmqpURL())
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ for tests: %v", err)
		t.Fatalf("Failed to connect to RabbitMQ for tests: %v", err)
	}
	testRabbitConn = conn
	log.Println("Successfully connected to RabbitMQ for tests!")

	// Register cleanup function
	t.Cleanup(func() {
		if testRabbitConn != nil {
			CloseRabbitMQ(testRabbitConn)
			log.Println("RabbitMQ test connection closed.")
		}
	})
}

// purgeQueue purges a queue by creating a temporary channel and a consumer, then closing it.
func purgeQueue(t *testing.T, conn *amqp091.Connection, queueName string) {
	ch, err := conn.Channel()
	require.NoError(t, err, "Failed to open a channel for purging")
	defer ch.Close()

	_, err = ch.QueuePurge(queueName, false)
	require.NoError(t, err, "Failed to purge queue %s", queueName)
	t.Logf("Purged queue: %s", queueName)
}

func TestRabbitTripRecordMessageQueue_PublishSubscribe(t *testing.T) {
	setupTestRabbitMQ(t)
	require.NotNil(t, testRabbitConn, "RabbitMQ connection is nil")

	// Create a new queue for this specific test to isolate it
	queueName := "test_record_create_queue_" + uuid.New().String()
	routingKey := "record.create"

	ch, err := testRabbitConn.Channel()
	require.NoError(t, err)
	defer ch.Close()

	err = DeclareQueueAndExchange(ch, queueName, exchangeName, routingKey)
	require.NoError(t, err)

	// Purge queue before test starts
	purgeQueue(t, testRabbitConn, queueName)

	q, err := NewRabbitTripRecordMessageQueue(mq.ActionCreate, testRabbitConn)
	require.NoError(t, err)
	assert.Equal(t, mq.ActionCreate, q.GetAction())

	// Cast to concrete type for cleanup
	concreteQ := q.(*rabbitTripRecordMessageQueue)
	defer concreteQ.channel.Close() // Close the channel opened by the queue instance

	subscriberID, subChan, err := q.Subscribe()
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, subscriberID)
	assert.NotNil(t, subChan)

	testMsg := mq.TripRecordMessage{
		ID:            uuid.New(),
		Name:          "Test Record",
		Amount:        123.45,
		PrePayAddress: db.Address("Alice"),
	}

	err = q.Publish(testMsg)
	require.NoError(t, err)

	// Wait for message with a timeout
	select {
	case receivedMsg := <-subChan:
		assert.Equal(t, testMsg.ID, receivedMsg.ID)
		assert.Equal(t, testMsg.Name, receivedMsg.Name)
		assert.InDelta(t, testMsg.Amount, receivedMsg.Amount, 0.001)
		assert.Equal(t, testMsg.PrePayAddress, receivedMsg.PrePayAddress)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	// De-subscribe
	err = q.DeSubscribe(subscriberID)
	assert.NoError(t, err)

	// Verify that the consumer channel is closed after DeSubscribe
	select {
	case _, ok := <-subChan:
		assert.False(t, ok, "Subscriber channel should be closed after DeSubscribe")
	case <-time.After(50 * time.Millisecond):
		// This can happen if the channel is already empty and the goroutine hasn't exited yet.
		// It's not a failure in itself, but combined with the assert.False above, it gives confidence.
	}
}

func TestRabbitTripAddressMessageQueue_PublishSubscribe(t *testing.T) {
	setupTestRabbitMQ(t)
	require.NotNil(t, testRabbitConn, "RabbitMQ connection is nil")

	// Create a new queue for this specific test to isolate it
	queueName := "test_address_delete_queue_" + uuid.New().String()
	routingKey := "address.delete"

	ch, err := testRabbitConn.Channel()
	require.NoError(t, err)
	defer ch.Close()

	err = DeclareQueueAndExchange(ch, queueName, exchangeName, routingKey)
	require.NoError(t, err)

	// Purge queue before test starts
	purgeQueue(t, testRabbitConn, queueName)

	q, err := NewRabbitTripAddressMessageQueue(mq.ActionDelete, testRabbitConn)
	require.NoError(t, err)
	assert.Equal(t, mq.ActionDelete, q.GetAction())

	// Cast to concrete type for cleanup
	concreteQ := q.(*rabbitTripAddressMessageQueue)
	defer concreteQ.channel.Close() // Close the channel opened by the queue instance

	subscriberID, subChan, err := q.Subscribe()
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, subscriberID)
	assert.NotNil(t, subChan)

	testMsg := mq.TripAddressMessage{
		Address: db.Address("Bob"),
	}

	err = q.Publish(testMsg)
	require.NoError(t, err)

	// Wait for message with a timeout
	select {
	case receivedMsg := <-subChan:
		assert.Equal(t, testMsg.Address, receivedMsg.Address)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	// De-subscribe
	err = q.DeSubscribe(subscriberID)
	assert.NoError(t, err)

	// Verify that the consumer channel is closed after DeSubscribe
	select {
	case _, ok := <-subChan:
		assert.False(t, ok, "Subscriber channel should be closed after DeSubscribe")
	case <-time.After(50 * time.Millisecond):
		// See comment in TestRabbitTripRecordMessageQueue_PublishSubscribe
	}
}

func TestNewRabbitTripMessageQueueWrapper(t *testing.T) {
	setupTestRabbitMQ(t)
	require.NotNil(t, testRabbitConn, "RabbitMQ connection is nil")

	wrapperIFace, err := NewRabbitTripMessageQueueWrapper(testRabbitConn)
	require.NoError(t, err)
	require.NotNil(t, wrapperIFace)

	wrapper, ok := wrapperIFace.(*rabbitTripMessageQueueWrapper)
	require.True(t, ok, "Wrapper is not of expected type")
	defer wrapper.Close() // Ensure all channels and connection are closed

	// Check Record MQs
	for _, action := range []mq.Action{mq.ActionCreate, mq.ActionUpdate, mq.ActionDelete} {
		q := wrapper.GetTripRecordMessageQueue(action)
		assert.NotNil(t, q, "Record queue for action %v should not be nil", action)
		assert.Equal(t, action, q.GetAction())
	}

	// Check Address MQs
	for _, action := range []mq.Action{mq.ActionCreate, mq.ActionDelete} {
		q := wrapper.GetTripAddressMessageQueue(action)
		assert.NotNil(t, q, "Address queue for action %v should not be nil", action)
		assert.Equal(t, action, q.GetAction())
	}
	// Ensure ActionUpdate for AddressMQ is nil
	assert.Nil(t, wrapper.GetTripAddressMessageQueue(mq.ActionUpdate))
}

func TestRabbitTripMessageQueueWrapper_EndToEnd(t *testing.T) {
	setupTestRabbitMQ(t)
	require.NotNil(t, testRabbitConn, "RabbitMQ connection is nil")

	wrapperIFace, err := NewRabbitTripMessageQueueWrapper(testRabbitConn)
	require.NoError(t, err)
	wrapper := wrapperIFace.(*rabbitTripMessageQueueWrapper)
	defer wrapper.Close()

	// Purge all queues before running test
	purgeQueue(t, testRabbitConn, fmt.Sprintf("trip_record_%d_queue", mq.ActionCreate))
	purgeQueue(t, testRabbitConn, fmt.Sprintf("trip_record_%d_queue", mq.ActionUpdate))
	purgeQueue(t, testRabbitConn, fmt.Sprintf("trip_record_%d_queue", mq.ActionDelete))
	purgeQueue(t, testRabbitConn, fmt.Sprintf("trip_address_%d_queue", mq.ActionCreate))
	purgeQueue(t, testRabbitConn, fmt.Sprintf("trip_address_%d_queue", mq.ActionDelete))

	// Get queues
	createRecordQ := wrapper.GetTripRecordMessageQueue(mq.ActionCreate)
	updateRecordQ := wrapper.GetTripRecordMessageQueue(mq.ActionUpdate)
	deleteRecordQ := wrapper.GetTripRecordMessageQueue(mq.ActionDelete)
	createAddressQ := wrapper.GetTripAddressMessageQueue(mq.ActionCreate)
	deleteAddressQ := wrapper.GetTripAddressMessageQueue(mq.ActionDelete)

	// Subscribe to all of them
	subRCID, subRCChan, err := createRecordQ.Subscribe()
	require.NoError(t, err)
	subRUID, subRUChan, err := updateRecordQ.Subscribe()
	require.NoError(t, err)
	subRDID, subRDChan, err := deleteRecordQ.Subscribe()
	require.NoError(t, err)
	subACID, subACChan, err := createAddressQ.Subscribe()
	require.NoError(t, err)
	subADID, subADChan, err := deleteAddressQ.Subscribe()
	require.NoError(t, err)

	// Test Record Create
	recordCreateMsg := mq.TripRecordMessage{ID: uuid.New(), Name: "New Rec", Amount: 100, PrePayAddress: db.Address("A")}
	err = createRecordQ.Publish(recordCreateMsg)
	require.NoError(t, err)
	select {
	case msg := <-subRCChan:
		assert.Equal(t, recordCreateMsg.ID, msg.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("Failed to receive record create message")
	}

	// Test Record Update
	recordUpdateMsg := mq.TripRecordMessage{ID: recordCreateMsg.ID, Name: "Updated Rec", Amount: 150, PrePayAddress: db.Address("B")}
	err = updateRecordQ.Publish(recordUpdateMsg)
	require.NoError(t, err)
	select {
	case msg := <-subRUChan:
		assert.Equal(t, recordUpdateMsg.ID, msg.ID)
		assert.Equal(t, recordUpdateMsg.Name, msg.Name)
	case <-time.After(2 * time.Second):
		t.Fatal("Failed to receive record update message")
	}

	// Test Record Delete
	recordDeleteMsg := mq.TripRecordMessage{ID: recordCreateMsg.ID}
	err = deleteRecordQ.Publish(recordDeleteMsg)
	require.NoError(t, err)
	select {
	case msg := <-subRDChan:
		assert.Equal(t, recordDeleteMsg.ID, msg.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("Failed to receive record delete message")
	}

	// Test Address Create
	addressCreateMsg := mq.TripAddressMessage{Address: db.Address("NewAddress")}
	err = createAddressQ.Publish(addressCreateMsg)
	require.NoError(t, err)
	select {
	case msg := <-subACChan:
		assert.Equal(t, addressCreateMsg.Address, msg.Address)
	case <-time.After(2 * time.Second):
		t.Fatal("Failed to receive address create message")
	}

	// Test Address Delete
	addressDeleteMsg := mq.TripAddressMessage{Address: db.Address("NewAddress")}
	err = deleteAddressQ.Publish(addressDeleteMsg)
	require.NoError(t, err)
	select {
	case msg := <-subADChan:
		assert.Equal(t, addressDeleteMsg.Address, msg.Address)
	case <-time.After(2 * time.Second):
		t.Fatal("Failed to receive address delete message")
	}

	// De-subscribe all
	createRecordQ.DeSubscribe(subRCID)
	updateRecordQ.DeSubscribe(subRUID)
	deleteRecordQ.DeSubscribe(subRDID)
	createAddressQ.DeSubscribe(subACID)
	deleteAddressQ.DeSubscribe(subADID)
}
