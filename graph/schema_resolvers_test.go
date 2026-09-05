package graph

import (
	"context"
	"strings"
	"testing"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/mq/mq"
	"dtm/domain"
	"dtm/graph/model"
	tripservice "dtm/services/trip"

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

func resolverWithChain(wrapper db.TripDBWrapper) *Resolver {
	provider := func(ctx context.Context) (db.Reader, error) {
		return db.TripDataLoaderFromContext(ctx)
	}
	return &Resolver{
		RecordFactory: tripservice.NewRecordFactory(provider),
		TripFactory:   tripservice.NewTripFactory(wrapper, provider),
	}
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
	_, err := (&tripResolver{Resolver: resolverWithChain(store)}).Records(resolverContext(store), &model.Trip{ID: tripID.String()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown RecordCategory")
}

func TestTripResolversShareRequestScopedLoaderCache(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	address, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	base := resolverWithChain(database)
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

type trackingAddressQueue struct {
	action   mq.Action
	messages []mq.TripAddressMessage
}

func (q *trackingAddressQueue) GetAction() mq.Action { return q.action }
func (q *trackingAddressQueue) Publish(message mq.TripAddressMessage) error {
	q.messages = append(q.messages, message)
	return nil
}
func (q *trackingAddressQueue) Subscribe(uuid.UUID) (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	return uuid.Nil, nil, nil
}
func (q *trackingAddressQueue) DeSubscribe(uuid.UUID) error { return nil }

type trackingMQ struct {
	queues        [mq.ActionCnt]trackingRecordQueue
	addressQueues [mq.ActionCnt]trackingAddressQueue
	reads         [mq.ActionCnt]int
}

func (m *trackingMQ) GetTripRecordMessageQueue(action mq.Action) mq.TripRecordMessageQueue {
	m.reads[action]++
	m.queues[action].action = action
	return &m.queues[action]
}
func (m *trackingMQ) GetTripAddressMessageQueue(action mq.Action) mq.TripAddressMessageQueue {
	m.addressQueues[action].action = action
	return &m.addressQueues[action]
}

type resolverTripFactory struct {
	trip        *resolverTrip
	createdName string
	selectedID  uuid.UUID
}

func (f *resolverTripFactory) Create(_ context.Context, name string) (tripservice.Trip, error) {
	f.createdName = name
	return f.trip, nil
}
func (f *resolverTripFactory) ForTrip(id uuid.UUID) tripservice.Trip {
	f.selectedID = id
	f.trip.id = id
	return f.trip
}

type resolverTrip struct {
	id               uuid.UUID
	updatedName      string
	createdName      string
	updatedAddressID uuid.UUID
	deletedAddressID uuid.UUID
}

func (t *resolverTrip) ID() uuid.UUID { return t.id }
func (t *resolverTrip) Info(context.Context) (*domain.TripInfo, error) {
	return &domain.TripInfo{ID: t.id}, nil
}
func (t *resolverTrip) UpdateInfo(_ context.Context, name string) (*domain.TripInfo, error) {
	t.updatedName = name
	return &domain.TripInfo{ID: t.id, Name: name}, nil
}
func (t *resolverTrip) Addresses(context.Context) ([]domain.Address, error) { return nil, nil }
func (t *resolverTrip) CreateAddress(_ context.Context, name string) (*domain.Address, error) {
	t.createdName = name
	return &domain.Address{ID: uuid.New(), Name: name}, nil
}
func (t *resolverTrip) UpdateAddress(_ context.Context, id uuid.UUID, name string) (*domain.Address, error) {
	t.updatedAddressID = id
	return &domain.Address{ID: id, Name: name}, nil
}
func (t *resolverTrip) DeleteAddress(_ context.Context, id uuid.UUID) (*domain.Address, error) {
	t.deletedAddressID = id
	return &domain.Address{ID: id, Name: "member"}, nil
}
func (t *resolverTrip) Append(context.Context, tripservice.Record) (tripservice.AppendResult, error) {
	return tripservice.AppendResult{}, nil
}
func (t *resolverTrip) List(context.Context) ([]tripservice.Record, error) { return nil, nil }
func (t *resolverTrip) CalculateMoneyShare(context.Context) (tripservice.MoneyShareResult, error) {
	return tripservice.MoneyShareResult{}, nil
}

func TestTripAndAddressMutationsUseTripAbstractions(t *testing.T) {
	tripID := uuid.New()
	trip := &resolverTrip{id: tripID}
	factory := &resolverTripFactory{trip: trip}
	queues := &trackingMQ{}
	resolver := &mutationResolver{Resolver: &Resolver{TripFactory: factory, TripMessageQueueWrapper: queues}}

	createdTrip, err := resolver.CreateTrip(context.Background(), model.NewTrip{Name: "holiday"})
	require.NoError(t, err)
	assert.Equal(t, tripID.String(), createdTrip.ID)
	assert.Equal(t, "holiday", factory.createdName)

	updatedTrip, err := resolver.UpdateTrip(context.Background(), tripID.String(), model.NewTrip{Name: "renamed"})
	require.NoError(t, err)
	assert.Equal(t, "renamed", updatedTrip.Name)
	assert.Equal(t, "renamed", trip.updatedName)
	assert.Equal(t, tripID, factory.selectedID)

	createdAddress, err := resolver.CreateAddress(context.Background(), tripID.String(), model.NewAddress{Name: "member"})
	require.NoError(t, err)
	assert.Equal(t, "member", trip.createdName)
	require.Len(t, queues.addressQueues[mq.ActionCreate].messages, 1)
	assert.Equal(t, createdAddress.ID, queues.addressQueues[mq.ActionCreate].messages[0].Address.ID.String())

	addressID := uuid.MustParse(createdAddress.ID)
	updatedAddress, err := resolver.UpdateAddress(context.Background(), tripID.String(), addressID.String(), model.NewAddress{Name: "updated member"})
	require.NoError(t, err)
	assert.Equal(t, addressID, trip.updatedAddressID)
	assert.Equal(t, "updated member", updatedAddress.Name)
	require.Len(t, queues.addressQueues[mq.ActionUpdate].messages, 1)

	deletedAddress, err := resolver.DeleteAddress(context.Background(), tripID.String(), addressID.String())
	require.NoError(t, err)
	assert.Equal(t, addressID, trip.deletedAddressID)
	assert.Equal(t, "member", deletedAddress.Name)
	require.Len(t, queues.addressQueues[mq.ActionDelete].messages, 1)
}

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
	base := resolverWithChain(database)
	base.TripMessageQueueWrapper = queues
	resolver := &mutationResolver{Resolver: base}
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
	_, err = database.AppendNew(context.Background(), tripID, domain.Record{
		RecordInfo: domain.RecordInfo{ID: recordID, Name: "broken", Amount: 0, PrePayAddress: *payer, Category: domain.CategoryNormal},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member}}},
	}, seedRecordPolicy{})
	require.NoError(t, err)

	category := model.RecordCategoryNormal
	oldInput := model.NewRecord{Name: "broken", Amount: 0, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}, Category: &category}
	newInput := oldInput
	newInput.Amount = 20
	queues := &trackingMQ{}
	base := resolverWithChain(database)
	base.TripMessageQueueWrapper = queues
	resolver := &mutationResolver{Resolver: base}

	updated, err := resolver.UpdateRecord(resolverContext(database), recordID.String(), model.EditRecord{Old: &oldInput, New: &newInput})
	require.NoError(t, err)
	assert.NotEqual(t, recordID.String(), updated.ID)
	require.NotNil(t, updated.Amount)
	assert.Equal(t, float64(20), *updated.Amount)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 1)
}

func TestStaleBaselineCannotAppendInvalidMaterializedTail(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := database.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	recordID := uuid.New()
	_, err = database.AppendNew(context.Background(), tripID, domain.Record{
		RecordInfo: domain.RecordInfo{ID: recordID, Name: "fixed", Amount: 20, PrePayAddress: *payer, Category: domain.CategoryFix},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member, ExtendMsg: 20}}},
	}, seedRecordPolicy{})
	require.NoError(t, err)

	normal := model.RecordCategoryNormal
	oldInput := model.NewRecord{Name: "fixed", Amount: 20, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}, Category: &normal}
	newInput := oldInput
	newInput.Amount = 10 // Valid as NORMAL, but only Amount is patched over the FIX tail.
	queues := &trackingMQ{}
	base := resolverWithChain(database)
	base.TripMessageQueueWrapper = queues
	_, err = (&mutationResolver{Resolver: base}).UpdateRecord(resolverContext(database), recordID.String(), model.EditRecord{Old: &oldInput, New: &newInput})
	require.ErrorIs(t, err, tripservice.ErrInvalidRecordSnapshot)
	assert.Empty(t, queues.queues[mq.ActionUpdate].messages)
}

type seedRecordPolicy struct{}

func (seedRecordPolicy) PrepareNew(value domain.Record, _ []domain.Address) (domain.Record, error) {
	return value, nil
}
func (seedRecordPolicy) ApplyPatch(domain.Record, domain.RecordPatch, []domain.Address) (domain.Record, bool, error) {
	panic("not used")
}
func (seedRecordPolicy) Validate(domain.Record) error { return nil }

func TestEquivalentStaleInputPreservesTailAndSkipsPublish(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := database.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	queues := &trackingMQ{}
	base := resolverWithChain(database)
	base.TripMessageQueueWrapper = queues
	resolver := &mutationResolver{Resolver: base}
	stamp := "1234"
	old := model.NewRecord{Name: "meal", Amount: 20, Time: &stamp, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}}
	created, err := resolver.CreateRecord(resolverContext(database), tripID.String(), old)
	require.NoError(t, err)

	latest := old
	latest.PrePayAddressID = member.ID.String()
	latest.ShouldPayAddressIds = []string{payer.ID.String()}
	later := "4567"
	latest.Time = &later
	first, err := resolver.UpdateRecord(resolverContext(database), created.ID, model.EditRecord{Old: &old, New: &latest})
	require.NoError(t, err)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 1)

	formatted := old
	formatted.PrePayAddressID = strings.ToUpper(old.PrePayAddressID)
	formatted.ShouldPayAddressIds = []string{strings.ToUpper(old.ShouldPayAddressIds[0])}
	formatted.ExtendPayMsg = []float64{0}
	formattedTime := "+001234"
	formatted.Time = &formattedTime
	noop, err := resolver.UpdateRecord(resolverContext(database), created.ID, model.EditRecord{Old: &old, New: &formatted})
	require.NoError(t, err)
	assert.Equal(t, first.ID, noop.ID)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 1)

	formatted.Name = "dinner"
	second, err := resolver.UpdateRecord(resolverContext(database), created.ID, model.EditRecord{Old: &old, New: &formatted})
	require.NoError(t, err)
	assert.Equal(t, first.ID, *second.ParentRecordID)
	assert.Equal(t, "dinner", *second.Name)
	assert.Equal(t, member.ID.String(), second.PrePayAddress.ID)
	assert.Equal(t, later, second.Time)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 2)
	message := queues.queues[mq.ActionUpdate].messages[1]
	require.Len(t, message.ShouldPayAddress, 1)
	assert.Equal(t, *payer, message.ShouldPayAddress[0].Address)
	assert.Equal(t, uuid.MustParse(first.ID), *message.ParentRecordID)
}
