package graph

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/mq/mq"
	"dtm/domain"
	"dtm/graph/model"
	"dtm/graph/utils"
	tripservice "dtm/services/trip"

	"github.com/99designs/gqlgen/client"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
)

type recordQueryDB struct {
	db.TripDBWrapper
	records map[uuid.UUID][]domain.RecordInfo
}

func (r *recordQueryDB) DataLoaderGetTripRecords(_ context.Context, ids []uuid.UUID, options db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	result := map[uuid.UUID][]db.RecordSnapshot{}
	for _, id := range ids {
		for _, info := range r.records[id] {
			if !options.HaveHistory && (info.ChildRecordID != nil || info.IsDeleted) {
				continue
			}
			result[id] = append(result[id], db.RecordSnapshot{TripID: id, Record: domain.Record{RecordInfo: info}})
		}
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
		trip, queryErr := query.Trip(ctx, tripID.String(), false)
		require.NoError(t, queryErr)
		assert.Equal(t, tripID.String(), trip.ID)
		assert.Equal(t, "trip", trip.Name)
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

func TestRecordIsValidRejectsInvalidPayload(t *testing.T) {
	record, err := utils.ToModelRecordChecked(domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Category: domain.CategoryNormal}}, false)
	require.NoError(t, err)
	assert.False(t, record.IsValid)
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
func (f *resolverTripFactory) ForTrip(id uuid.UUID, _ ...tripservice.ReadOptions) tripservice.Trip {
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

func (t *resolverTrip) CalculateAddressStatistics(context.Context) (map[uuid.UUID]tripservice.AddressStatistics, error) {
	return nil, nil
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

	require.Len(t, updated.ShouldPayAddress, 1)
	assert.Equal(t, member.ID.String(), updated.ShouldPayAddress[0].ID)
	assert.Equal(t, []float64{0}, updated.ExtendPayMsg)
	assert.True(t, updated.IsValid)
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
		RecordInfo: domain.RecordInfo{ID: recordID, Name: "broken\n", Amount: 0, PrePayAddress: *payer, Category: domain.CategoryNormal},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member}}},
	}, seedRecordPolicy{})
	require.NoError(t, err)

	category := model.RecordCategoryNormal
	oldInput := model.NewRecord{Name: "broken\n", Amount: 0, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}, Category: &category}
	newInput := oldInput
	newInput.Amount = 20
	newInput.Name = " <repaired> 👩🏽‍💻 "
	queues := &trackingMQ{}
	base := resolverWithChain(database)
	base.TripMessageQueueWrapper = queues
	resolver := &mutationResolver{Resolver: base}

	updated, err := resolver.UpdateRecord(resolverContext(database), recordID.String(), model.EditRecord{Old: &oldInput, New: &newInput})
	require.NoError(t, err)
	assert.NotEqual(t, recordID.String(), updated.ID)
	require.NotNil(t, updated.Name)
	assert.Equal(t, newInput.Name, *updated.Name)
	require.NotNil(t, updated.Amount)
	assert.Equal(t, float64(20), *updated.Amount)
	require.Len(t, queues.queues[mq.ActionUpdate].messages, 1)
}

func TestStaleBaselineCanAppendRepairableMaterializedTail(t *testing.T) {
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
	updated, err := (&mutationResolver{Resolver: base}).UpdateRecord(resolverContext(database), recordID.String(), model.EditRecord{Old: &oldInput, New: &newInput})
	require.NoError(t, err)
	assert.False(t, updated.IsValid)
	assert.Len(t, queues.queues[mq.ActionUpdate].messages, 1)
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

type addressStatisticsResult struct {
	ID         string
	Name       string
	Statistics *model.AddressStatistics
}

type statisticsCountingFactory struct {
	tripservice.TripFactory
	calls atomic.Int32
}

func (f *statisticsCountingFactory) ForTrip(id uuid.UUID, options ...tripservice.ReadOptions) tripservice.Trip {
	return &statisticsCountingTrip{Trip: f.TripFactory.ForTrip(id, options...), calls: &f.calls}
}

type statisticsCountingTrip struct {
	tripservice.Trip
	calls *atomic.Int32
}

func (t *statisticsCountingTrip) CalculateAddressStatistics(ctx context.Context) (map[uuid.UUID]tripservice.AddressStatistics, error) {
	t.calls.Add(1)
	return t.Trip.CalculateAddressStatistics(ctx)
}

func TestTripAddressStatisticsSelectionAndHistory(t *testing.T) {
	store := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "statistics"}))
	members := make([]domain.Address, 4)
	for i, name := range []string{"A", "B", "C", "Unused"} {
		address, err := store.CreateAddress(tripID, name)
		require.NoError(t, err)
		members[i] = *address
	}
	base := resolverWithChain(store)
	ctx := resolverContext(store)
	trip := base.TripFactory.ForTrip(tripID)
	expense := domain.Record{RecordInfo: domain.RecordInfo{Name: "meal", Amount: 900, PrePayAddress: members[0]}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: members[0]}, {Address: members[1]}, {Address: members[2]}}}}
	intent, err := base.RecordFactory.New(ctx, tripID, expense)
	require.NoError(t, err)
	_, err = trip.Append(ctx, intent)
	require.NoError(t, err)
	transfer := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "transfer", Amount: 100, PrePayAddress: members[1], Category: domain.CategoryTransfer}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: members[0], ExtendMsg: 100}}}}
	_, err = store.AppendNew(ctx, tripID, transfer, seedRecordPolicy{})
	require.NoError(t, err)
	deleted := expense
	deleted.ID = uuid.New()
	deleted.IsDeleted = true
	_, err = store.AppendNew(ctx, tripID, deleted, seedRecordPolicy{})
	require.NoError(t, err)
	invalid := expense
	invalid.ID = uuid.New()
	invalid.Category = domain.CategoryFix // Fixed shares are all zero and do not balance.
	_, err = store.AppendNew(ctx, tripID, invalid, seedRecordPolicy{})
	require.NoError(t, err)
	factory := &statisticsCountingFactory{TripFactory: base.TripFactory}
	base.TripFactory = factory
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: base}))
	c := client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(db.WithTripDataLoader(r.Context(), db.NewTripDataLoader(store))))
	}))
	for _, selection := range []string{
		"id name",
		"id statistics @skip(if: true) { totalExpense }",
		"id ... on Address @include(if: false) { statistics { totalExpense } }",
	} {
		db.DataLoaderDebug.Reset()
		var response any
		require.NoError(t, c.Post(fmt.Sprintf(`{ trip(tripId:%q) { addresses { %s } } }`, tripID, selection), &response))
		require.Zero(t, factory.calls.Load())
		require.Zero(t, db.DataLoaderDebug.Snapshot().TripRecords.Batches)
	}
	type addressResult struct {
		ID    string
		Stats model.AddressStatistics
		Again model.AddressStatistics
	}
	var live []addressResult
	for _, history := range []bool{false, true} {
		factory.calls.Store(0)
		db.DataLoaderDebug.Reset()
		var response struct {
			Trip struct {
				Addresses  []addressResult
				IsValid    bool
				MoneyShare []model.Tx
				Records    []struct {
					PrePayAddress    addressStatisticsResult
					ShouldPayAddress []addressStatisticsResult
				}
			}
		}
		query := fmt.Sprintf(`{
   trip(tripId:%q,haveHistory:%t) {
    addresses { id ...Stats ... on Address { again:statistics { totalExpense totalPrepaid activeRecordCount } } }
    isValid moneyShare { output { amount } }
    records { prePayAddress { id statistics { totalExpense totalPrepaid activeRecordCount } } shouldPayAddress { id statistics { totalExpense totalPrepaid activeRecordCount } } }
   }
  }
  fragment Stats on Address { stats:statistics { totalExpense totalPrepaid activeRecordCount } }`, tripID, history)
		require.NoError(t, c.Post(query, &response))
		calls := len(response.Trip.Addresses) * 2
		for _, record := range response.Trip.Records {
			calls += 1 + len(record.ShouldPayAddress)
		}
		require.Equal(t, int32(calls), factory.calls.Load(), "each selected statistics field calculates through the shared DB loader")
		require.Equal(t, int64(1), db.DataLoaderDebug.Snapshot().TripRecords.Batches)
		require.False(t, response.Trip.IsValid)
		require.Empty(t, response.Trip.MoneyShare)
		expected := map[string]model.AddressStatistics{
			members[0].ID.String(): {TotalExpense: 400, TotalPrepaid: 900, ActiveRecordCount: 2},
			members[1].ID.String(): {TotalExpense: 300, TotalPrepaid: 100, ActiveRecordCount: 2},
			members[2].ID.String(): {TotalExpense: 300, ActiveRecordCount: 1},
			members[3].ID.String(): {},
		}
		require.Len(t, response.Trip.Addresses, 4)
		for _, address := range response.Trip.Addresses {
			require.Equal(t, expected[address.ID], address.Stats)
			require.Equal(t, address.Stats, address.Again)
		}
		for _, record := range response.Trip.Records {
			require.Equal(t, expected[record.PrePayAddress.ID], *record.PrePayAddress.Statistics)
			for _, member := range record.ShouldPayAddress {
				require.Equal(t, expected[member.ID], *member.Statistics)
			}
		}
		if !history {
			live = response.Trip.Addresses
		} else {
			require.ElementsMatch(t, live, response.Trip.Addresses)
		}
	}
}

type statisticsReadFailure struct{ db.TripDBWrapper }

func (s statisticsReadFailure) DataLoaderGetTripRecords(context.Context, []uuid.UUID, db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	return nil, fmt.Errorf("statistics storage unavailable")
}

func TestAddressStatisticsReadFailurePreservesAddress(t *testing.T) {
	store := mem.NewInMemoryTripDBWrapper()
	id := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: id, Name: "errors"}))
	address, err := store.CreateAddress(id, "member")
	require.NoError(t, err)
	failing := statisticsReadFailure{TripDBWrapper: store}
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: resolverWithChain(failing)}))
	c := client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(db.WithTripDataLoader(r.Context(), db.NewTripDataLoader(failing))))
	}))
	var response struct {
		Trip struct{ Addresses []addressStatisticsResult }
	}
	err = c.Post(fmt.Sprintf(`{ trip(tripId:%q) { addresses { id name statistics { totalExpense } } } }`, id), &response)
	require.ErrorContains(t, err, "failed to calculate address statistics")
	require.Len(t, response.Trip.Addresses, 1)
	require.Equal(t, address.ID.String(), response.Trip.Addresses[0].ID)
	require.Nil(t, response.Trip.Addresses[0].Statistics)
}

func TestSnapshotsRejectQueryFields(t *testing.T) {
	schema := NewExecutableSchema(Config{Resolvers: &Resolver{}}).Schema()
	for _, operation := range []string{
		`mutation { createAddress(tripId:"x", input:{name:"A"}) { statistics { totalExpense } } }`,
		`mutation { updateAddress(tripId:"x", addressId:"x", input:{name:"A"}) { statistics { totalExpense } } }`,
		`mutation { deleteAddress(tripId:"x", addressId:"x") { statistics { totalExpense } } }`,
		`mutation { createRecord(tripId:"x", input:{name:"R", amount:1, prePayAddressId:"x", shouldPayAddressIds:[]}) { prePayAddress { statistics { totalExpense } } } }`,
		`mutation { updateRecord(recordId:"x", input:{}) { shouldPayAddress { statistics { totalExpense } } } }`,
		`subscription { subAddressCreate(tripId:"x") { statistics { totalExpense } } }`,
		`subscription { subAddressUpdate(tripId:"x") { statistics { totalExpense } } }`,
		`subscription { subAddressDelete(tripId:"x") { statistics { totalExpense } } }`,
		`subscription { subRecordCreate(tripId:"x") { prePayAddress { statistics { totalExpense } } } }`,
		`subscription { subRecordUpdate(tripId:"x") { shouldPayAddress { statistics { totalExpense } } } }`,
		`mutation { createTrip(input:{name:"T"}) { addresses { id } } }`,
		`mutation { updateTrip(tripId:"x", input:{name:"T"}) { records { id } } }`,
	} {
		t.Run(operation, func(t *testing.T) {
			_, errs := gqlparser.LoadQuery(schema, operation)
			require.NotEmpty(t, errs)
			require.Contains(t, errs.Error(), "Cannot query field")
		})
	}
}

func TestMutationSnapshotsDoNotCalculateStatistics(t *testing.T) {
	store := mem.NewInMemoryTripDBWrapper()
	base := resolverWithChain(store)
	factory := &statisticsCountingFactory{TripFactory: base.TripFactory}
	base.TripFactory = factory
	base.TripMessageQueueWrapper = &trackingMQ{}
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: base}))
	c := client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(db.WithTripDataLoader(r.Context(), db.NewTripDataLoader(store))))
	}))
	var trip struct {
		CreateTrip struct{ ID, Name, Typename string }
	}
	require.NoError(t, c.Post(`mutation { createTrip(input:{name:"T"}) { id name typename:__typename } }`, &trip))
	require.Equal(t, "TripSnapshot", trip.CreateTrip.Typename)
	id := trip.CreateTrip.ID
	var address struct{ CreateAddress struct{ ID, Typename string } }
	require.NoError(t, c.Post(fmt.Sprintf(`mutation { createAddress(tripId:%q,input:{name:"A"}) { id typename:__typename } }`, id), &address))
	require.Equal(t, "AddressSnapshot", address.CreateAddress.Typename)
	input := fmt.Sprintf(`{name:"R",amount:10,prePayAddressId:%q,shouldPayAddressIds:[%q]}`, address.CreateAddress.ID, address.CreateAddress.ID)
	var record struct {
		CreateRecord struct {
			ID, Typename  string
			PrePayAddress struct{ Typename string }
		}
	}
	require.NoError(t, c.Post(fmt.Sprintf(`mutation { createRecord(tripId:%q,input:%s) { id typename:__typename prePayAddress { typename:__typename } } }`, id, input), &record))
	require.Equal(t, "RecordSnapshot", record.CreateRecord.Typename)
	require.Equal(t, "AddressSnapshot", record.CreateRecord.PrePayAddress.Typename)
	unused, err := store.CreateAddress(uuid.MustParse(id), "Unused")
	require.NoError(t, err)
	for _, operation := range []string{
		fmt.Sprintf(`mutation { updateTrip(tripId:%q,input:{name:"Renamed"}) { id name } }`, id),
		fmt.Sprintf(`mutation { updateAddress(tripId:%q,addressId:%q,input:{name:"B"}) { id name } }`, id, address.CreateAddress.ID),
		fmt.Sprintf(`mutation { updateRecord(recordId:%q,input:{old:%s,new:%s}) { id isActive prePayAddress { id name } shouldPayAddress { id name } } }`, record.CreateRecord.ID, input, input),
		fmt.Sprintf(`mutation { deleteAddress(tripId:%q,addressId:%q) { id name } }`, id, unused.ID.String()),
	} {
		var response any
		require.NoError(t, c.Post(operation, &response))
	}
	require.Zero(t, factory.calls.Load())
}

type snapshotRecordQueue struct {
	trackingRecordQueue
	message mq.TripRecordMessage
}

func (q *snapshotRecordQueue) Subscribe(uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	ch := make(chan mq.TripRecordMessage, 1)
	ch <- q.message
	close(ch)
	return uuid.New(), ch, nil
}

type snapshotAddressQueue struct {
	trackingAddressQueue
	message mq.TripAddressMessage
}

func (q *snapshotAddressQueue) Subscribe(uuid.UUID) (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	ch := make(chan mq.TripAddressMessage, 1)
	ch <- q.message
	close(ch)
	return uuid.New(), ch, nil
}

type snapshotQueues struct {
	record  snapshotRecordQueue
	address snapshotAddressQueue
}

func (q *snapshotQueues) GetTripRecordMessageQueue(mq.Action) mq.TripRecordMessageQueue {
	return &q.record
}
func (q *snapshotQueues) GetTripAddressMessageQueue(mq.Action) mq.TripAddressMessageQueue {
	return &q.address
}

func TestSubscriptionSnapshotsDoNotCalculateStatistics(t *testing.T) {
	factory := &statisticsCountingFactory{}
	member := domain.Address{ID: uuid.New(), Name: "A"}
	queues := &snapshotQueues{
		record: snapshotRecordQueue{message: mq.TripRecordMessage{
			ID: uuid.New(), Name: "R", Amount: 10, PrePayAddress: member,
			ShouldPayAddress: []domain.ExtendAddress{{Address: member, ExtendMsg: 10}},
		}},
		address: snapshotAddressQueue{message: mq.TripAddressMessage{Address: member}},
	}
	resolver := &subscriptionResolver{Resolver: &Resolver{TripFactory: factory, TripMessageQueueWrapper: queues}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, subscribe := range []func(context.Context, string) (<-chan *model.RecordSnapshot, error){
		resolver.SubRecordCreate, resolver.SubRecordUpdate,
	} {
		stream, err := subscribe(ctx, uuid.NewString())
		require.NoError(t, err)
		select {
		case record := <-stream:
			require.NotNil(t, record)
			require.Equal(t, member.ID.String(), record.PrePayAddress.ID)
			require.Equal(t, member.Name, record.ShouldPayAddress[0].Name)
		case <-ctx.Done():
			t.Fatal("record snapshot was not delivered")
		}
	}
	for _, subscribe := range []func(context.Context, string) (<-chan *model.AddressSnapshot, error){
		resolver.SubAddressCreate, resolver.SubAddressUpdate, resolver.SubAddressDelete,
	} {
		stream, err := subscribe(ctx, uuid.NewString())
		require.NoError(t, err)
		select {
		case address := <-stream:
			require.Equal(t, &model.AddressSnapshot{ID: member.ID.String(), Name: member.Name}, address)
		case <-ctx.Done():
			t.Fatal("address snapshot was not delivered")
		}
	}
	require.Zero(t, factory.calls.Load())
}
