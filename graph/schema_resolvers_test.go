package graph

import (
	"context"
	"testing"

	"dtm/db/db"
	"dtm/domain"
	"dtm/graph/model"
	"dtm/mq/mq"

	"github.com/google/uuid"
	"github.com/r3labs/diff/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingTripDB struct {
	db.TripDBWrapper
	updateCalls int
	tripIDReads int
	tripID      uuid.UUID
	addresses   []domain.Address
}

func (db *trackingTripDB) UpdateTripRecord(_ uuid.UUID, _ diff.Changelog) (uuid.UUID, error) {
	db.updateCalls++
	return db.tripID, nil
}

func (db *trackingTripDB) GetTripAddressList(_ uuid.UUID) ([]domain.Address, error) {
	return db.addresses, nil
}

func (db *trackingTripDB) GetRecordTripID(_ uuid.UUID) (uuid.UUID, error) {
	db.tripIDReads++
	return db.tripID, nil
}

type trackingTripRecordQueue struct {
	publishCalls int
}

func (queue *trackingTripRecordQueue) GetAction() mq.Action {
	return mq.ActionUpdate
}

func (queue *trackingTripRecordQueue) Publish(_ mq.TripRecordMessage) error {
	queue.publishCalls++
	return nil
}

func (queue *trackingTripRecordQueue) Subscribe(_ uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	return uuid.Nil, nil, nil
}

func (queue *trackingTripRecordQueue) DeSubscribe(_ uuid.UUID) error {
	return nil
}

type trackingTripMessageQueueWrapper struct {
	mq.TripMessageQueueWrapper
	recordQueueRequests int
	recordQueue         *trackingTripRecordQueue
}

func (wrapper *trackingTripMessageQueueWrapper) GetTripRecordMessageQueue(_ mq.Action) mq.TripRecordMessageQueue {
	wrapper.recordQueueRequests++
	return wrapper.recordQueue
}

func TestUpdateRecordNoOpHasNoPersistenceOrMQSideEffects(t *testing.T) {
	recordID := uuid.New()
	tripID := uuid.New()
	prePayAddress := domain.Address{ID: uuid.New(), Name: "Alice"}
	shouldPayAddress := domain.Address{ID: uuid.New(), Name: "Bob"}
	timestamp := "1700000000000"
	category := model.RecordCategoryNormal

	oldInput := &model.NewRecord{
		Name:                "No-op record",
		Amount:              42,
		PrePayAddressID:     prePayAddress.ID.String(),
		Time:                &timestamp,
		ShouldPayAddressIds: []string{shouldPayAddress.ID.String()},
		ExtendPayMsg:        []float64{0},
		Category:            &category,
	}
	newInput := &model.NewRecord{
		Name:                oldInput.Name,
		Amount:              oldInput.Amount,
		PrePayAddressID:     oldInput.PrePayAddressID,
		Time:                &timestamp,
		ShouldPayAddressIds: append([]string(nil), oldInput.ShouldPayAddressIds...),
		ExtendPayMsg:        append([]float64(nil), oldInput.ExtendPayMsg...),
		Category:            &category,
	}

	database := &trackingTripDB{
		tripID:    tripID,
		addresses: []domain.Address{prePayAddress, shouldPayAddress},
	}
	queue := &trackingTripRecordQueue{}
	messageQueues := &trackingTripMessageQueueWrapper{recordQueue: queue}
	resolver := &mutationResolver{Resolver: &Resolver{
		TripDB:                  database,
		TripMessageQueueWrapper: messageQueues,
	}}

	result, err := resolver.UpdateRecord(context.Background(), recordID.String(), model.EditRecord{
		Old: oldInput,
		New: newInput,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, recordID.String(), result.ID)
	require.NotNil(t, result.PrePayAddress)
	assert.Equal(t, prePayAddress.ID.String(), result.PrePayAddress.ID)
	assert.Equal(t, prePayAddress.Name, result.PrePayAddress.Name)
	assert.Equal(t, 0, database.updateCalls)
	assert.Equal(t, 1, database.tripIDReads)
	assert.Equal(t, 0, messageQueues.recordQueueRequests)
	assert.Equal(t, 0, queue.publishCalls)
}
