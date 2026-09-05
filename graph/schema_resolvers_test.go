package graph

import (
	"context"
	"testing"

	"dtm/db/db"
	"dtm/db/mem"
	"dtm/domain"
	"dtm/graph/model"
	"dtm/mq/mq"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordQueryDB struct {
	db.TripDBWrapper
	records map[uuid.UUID][]domain.RecordInfo
}

func (r *recordQueryDB) DataLoaderGetRecordInfoList(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.RecordInfo, error) {
	result := map[uuid.UUID][]domain.RecordInfo{}
	for _, id := range ids {
		result[id] = r.records[id]
	}
	return result, nil
}

func resolverContext(wrapper db.TripDBWrapper) context.Context {
	return db.WithTripDataLoader(context.Background(), db.NewTripDataLoader(wrapper))
}

func resetResolverDataLoader(t *testing.T, ctx context.Context) {
	t.Helper()
	loader, err := db.TripDataLoaderFromContext(ctx)
	require.NoError(t, err)
	loader.Reset()
}

func TestTripRecordsReturnsErrorForUnknownCategory(t *testing.T) {
	tripID := uuid.New()
	store := &recordQueryDB{records: map[uuid.UUID][]domain.RecordInfo{tripID: {{ID: uuid.New(), Category: domain.RecordCategory(99)}}}}
	_, err := (&tripResolver{Resolver: &Resolver{TripDB: store}}).Records(resolverContext(store), &model.Trip{ID: tripID.String()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown RecordCategory")
}

func TestTripResolversShareRequestScopedLoaderCache(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	address, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	base := &Resolver{TripDB: database}
	ctx := resolverContext(database)
	db.DataLoaderDebug.Reset()

	query := &queryResolver{Resolver: base}
	for range 2 {
		trip, queryErr := query.Trip(ctx, tripID.String())
		require.NoError(t, queryErr)
		assert.Equal(t, &model.Trip{ID: tripID.String(), Name: "trip"}, trip)
	}

	fields := &tripResolver{Resolver: base}
	for range 2 {
		addresses, addressErr := fields.Addresses(ctx, &model.Trip{ID: tripID.String()})
		require.NoError(t, addressErr)
		require.Len(t, addresses, 1)
		assert.Equal(t, address.ID.String(), addresses[0].ID)
	}

	loads := db.DataLoaderDebug.Snapshot()
	assert.Equal(t, db.DataLoadCount{Batches: 1, Keys: 1}, loads.Trips)
	assert.Equal(t, db.DataLoadCount{Batches: 1, Keys: 1}, loads.TripAddresses)
}

func TestRecordIsValidRejectsInvalidEventBeforeReadingPayload(t *testing.T) {
	valid, err := (&recordResolver{Resolver: &Resolver{}}).IsValid(context.Background(), &model.Record{ID: uuid.NewString(), Category: model.RecordCategoryNormal, EventValid: false})
	require.NoError(t, err)
	assert.False(t, valid)
}

type trackingRecordQueue struct {
	action   mq.Action
	messages []mq.TripRecordMessage
}

func (q *trackingRecordQueue) GetAction() mq.Action { return q.action }
func (q *trackingRecordQueue) Publish(message mq.TripRecordMessage) error {
	q.messages = append(q.messages, message)
	return nil
}
func (q *trackingRecordQueue) Subscribe(uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	return uuid.Nil, nil, nil
}
func (q *trackingRecordQueue) DeSubscribe(uuid.UUID) error { return nil }

type trackingMQ struct {
	queues [mq.ActionCnt]trackingRecordQueue
	reads  [mq.ActionCnt]int
}

func (m *trackingMQ) GetTripRecordMessageQueue(action mq.Action) mq.TripRecordMessageQueue {
	m.reads[action]++
	m.queues[action].action = action
	return &m.queues[action]
}
func (m *trackingMQ) GetTripAddressMessageQueue(mq.Action) mq.TripAddressMessageQueue { return nil }

func TestRecordMutationsKeepMQIdentityAndSkipNoOpPublish(t *testing.T) {
	db.DataLoaderDebug.Reset()
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := database.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	queues := &trackingMQ{}
	resolver := &mutationResolver{Resolver: &Resolver{TripDB: database, ChainStore: database, TripMessageQueueWrapper: queues}}
	category := model.RecordCategoryNormal
	input := model.NewRecord{Name: "meal", Amount: 20, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}, Category: &category}
	ctx := resolverContext(database)

	created, err := resolver.CreateRecord(ctx, tripID.String(), input)
	require.NoError(t, err)
	resetResolverDataLoader(t, ctx)
	db.DataLoaderDebug.Reset()
	require.Len(t, queues.queues[mq.ActionCreate].messages, 1)
	assert.Equal(t, created.ID, queues.queues[mq.ActionCreate].messages[0].ID.String())
	assert.Equal(t, tripID, queues.queues[mq.ActionCreate].messages[0].TripID)

	updated, err := resolver.UpdateRecord(ctx, created.ID, model.EditRecord{Old: &input, New: &input})
	require.NoError(t, err)
	updateLoads := db.DataLoaderDebug.Snapshot()
	assert.Equal(t, db.DataLoadCount{Batches: 1, Keys: 1}, updateLoads.Records)
	assert.Zero(t, updateLoads.TripAddresses, "UpdateRecord must leave address canonicalization to AppendPatch")
	resetResolverDataLoader(t, ctx)
	assert.Equal(t, created.ID, updated.ID)
	assert.Zero(t, queues.reads[mq.ActionUpdate])
	assert.Empty(t, queues.queues[mq.ActionUpdate].messages)

	changed := input
	changed.Name = "dinner"
	updated, err = resolver.UpdateRecord(ctx, created.ID, model.EditRecord{Old: &input, New: &changed})
	require.NoError(t, err)
	resetResolverDataLoader(t, ctx)
	require.NotEqual(t, created.ID, updated.ID)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 1)

	latest, err := resolver.UpdateRecord(ctx, created.ID, model.EditRecord{Old: &changed, New: &changed})
	require.NoError(t, err)
	resetResolverDataLoader(t, ctx)
	assert.Equal(t, updated.ID, latest.ID)
	assert.True(t, latest.IsActive)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 1)

	recordLoadsBeforePayload := db.DataLoaderDebug.Snapshot().Records
	fields := &recordResolver{Resolver: resolver.Resolver}
	shouldPay, err := fields.ShouldPayAddress(ctx, updated)
	require.NoError(t, err)
	require.Len(t, shouldPay, 1)
	assert.Equal(t, member.ID.String(), shouldPay[0].ID)
	extendPay, err := fields.ExtendPayMsg(ctx, updated)
	require.NoError(t, err)
	assert.Equal(t, []float64{0}, extendPay)
	recordLoadsAfterPayload := db.DataLoaderDebug.Snapshot().Records
	assert.Equal(t, recordLoadsBeforePayload, recordLoadsAfterPayload)
	t.Logf("record backing loads before payload fields=%+v after=%+v", recordLoadsBeforePayload, recordLoadsAfterPayload)
}

func TestUpdateRecordAllowsInvalidOldInputToRepairActiveRecord(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := database.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	recordID := uuid.New()
	require.NoError(t, database.AppendNew(context.Background(), tripID, domain.Record{
		RecordInfo: domain.RecordInfo{ID: recordID, Name: "broken", Amount: 0, PrePayAddress: *payer, Category: domain.CategoryNormal},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member}}},
	}))

	category := model.RecordCategoryNormal
	oldInput := model.NewRecord{Name: "broken", Amount: 0, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}, Category: &category}
	newInput := oldInput
	newInput.Amount = 20
	queues := &trackingMQ{}
	resolver := &mutationResolver{Resolver: &Resolver{TripDB: database, ChainStore: database, TripMessageQueueWrapper: queues}}

	updated, err := resolver.UpdateRecord(resolverContext(database), recordID.String(), model.EditRecord{Old: &oldInput, New: &newInput})
	require.NoError(t, err)
	assert.NotEqual(t, recordID.String(), updated.ID)
	require.NotNil(t, updated.Amount)
	assert.Equal(t, float64(20), *updated.Amount)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 1)
}

func TestStaleBaselineMayAppendInvalidMaterializedTail(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := database.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	recordID := uuid.New()
	require.NoError(t, database.AppendNew(context.Background(), tripID, domain.Record{
		RecordInfo: domain.RecordInfo{ID: recordID, Name: "fixed", Amount: 20, PrePayAddress: *payer, Category: domain.CategoryFix},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member, ExtendMsg: 20}}},
	}))

	normal := model.RecordCategoryNormal
	oldInput := model.NewRecord{Name: "fixed", Amount: 20, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}, Category: &normal}
	newInput := oldInput
	newInput.Amount = 10 // Valid as NORMAL, but only Amount is patched over the FIX tail.
	queues := &trackingMQ{}
	base := &Resolver{TripDB: database, ChainStore: database, TripMessageQueueWrapper: queues}
	updated, err := (&mutationResolver{Resolver: base}).UpdateRecord(resolverContext(database), recordID.String(), model.EditRecord{Old: &oldInput, New: &newInput})
	require.NoError(t, err)
	assert.NotEqual(t, recordID.String(), updated.ID)

	valid, err := (&recordResolver{Resolver: base}).IsValid(resolverContext(database), updated)
	require.NoError(t, err)
	assert.False(t, valid)
}
