package graph

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/db/testutil"
	"dtm/adapters/mq/mq"
	"dtm/config"
	"dtm/domain"
	"dtm/graph/model"
	"dtm/graph/utils"
	"dtm/libs/recordpatch"
	"dtm/libs/textvalidate"
	"dtm/services/auth"
	tripservice "dtm/services/trip"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/99designs/gqlgen/client"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func resolverContext(wrapper db.TripDBWrapper, counters ...*testutil.LoaderCounters) context.Context {
	var store db.DataLoaderStore = wrapper
	if len(counters) > 0 {
		store = counters[0].Wrap(store)
	}
	return db.WithTripDataLoader(context.Background(), db.NewTripDataLoader(store))
}

func resolverWithChain(wrapper db.TripDBWrapper) *Resolver {
	provider := func(ctx context.Context) (db.Reader, error) {
		return db.TripDataLoaderFromContext(ctx)
	}
	return &Resolver{
		RecordFactory: tripservice.NewRecordFactory(provider),
		TripFactory:   tripservice.NewTripFactory(wrapper, nil, provider),
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
	counts := new(testutil.LoaderCounters)
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	address, err := database.CreateAddress(context.Background(), tripID, "member")
	require.NoError(t, err)
	base := resolverWithChain(database)
	ctx := resolverContext(database, counts)
	counts.Reset()

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

	loads := counts.Snapshot()
	assert.Equal(t, testutil.DataLoadCount{Batches: 1, Keys: 1}, loads.Trips)
	assert.Equal(t, testutil.DataLoadCount{Batches: 1, Keys: 1}, loads.TripAddresses)
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
	db.BindingStore
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

	updatedTrip, err := resolver.UpdateTrip(context.Background(), tripID.String(), model.EditTrip{Name: profileNamePtr("renamed")})
	require.NoError(t, err)
	assert.Equal(t, "renamed", updatedTrip.Name)
	assert.Equal(t, "renamed", trip.updatedName)
	assert.Equal(t, tripID, factory.selectedID)

	createdAddress, err := resolver.CreateAddress(context.Background(), tripID.String(), model.NewAddress{Name: profileNamePtr("member")}, false)
	require.NoError(t, err)
	assert.Equal(t, "member", trip.createdName)
	require.Len(t, queues.addressQueues[mq.ActionCreate].messages, 1)
	assert.Equal(t, createdAddress.ID, queues.addressQueues[mq.ActionCreate].messages[0].Address.ID.String())

	addressID := uuid.MustParse(createdAddress.ID)
	updatedAddress, err := resolver.UpdateAddress(context.Background(), tripID.String(), addressID.String(), model.NewAddress{Name: profileNamePtr("updated member")})
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
	counts := new(testutil.LoaderCounters)
	counts.Reset()
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := database.CreateAddress(context.Background(), tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(context.Background(), tripID, "member")
	require.NoError(t, err)
	queues := &trackingMQ{}
	base := resolverWithChain(database)
	base.TripMessageQueueWrapper = queues
	resolver := &mutationResolver{Resolver: base}
	category := model.RecordCategoryNormal
	input := model.NewRecord{Name: "meal", Amount: 20, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{member.ID.String()}, Category: &category}
	ctx := resolverContext(database, counts)

	created, err := resolver.CreateRecord(ctx, tripID.String(), input)
	require.NoError(t, err)
	resetResolverDataLoader(t, ctx)
	counts.Reset()
	require.Len(t, queues.queues[mq.ActionCreate].messages, 1)
	assert.Equal(t, created.ID, queues.queues[mq.ActionCreate].messages[0].ID.String())
	assert.Equal(t, tripID, queues.queues[mq.ActionCreate].messages[0].TripID)

	updated, err := resolver.UpdateRecord(ctx, created.ID, model.EditRecord{Old: &input, New: &input})
	require.NoError(t, err)
	updateLoads := counts.Snapshot()
	assert.Equal(t, testutil.DataLoadCount{Batches: 1, Keys: 1}, updateLoads.Records)
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
	payer, err := database.CreateAddress(context.Background(), tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(context.Background(), tripID, "member")
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
	payer, err := database.CreateAddress(context.Background(), tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(context.Background(), tripID, "member")
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
	payer, err := database.CreateAddress(context.Background(), tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(context.Background(), tripID, "member")
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
	counts := new(testutil.LoaderCounters)
	store := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "statistics"}))
	members := make([]domain.Address, 4)
	for i, name := range []string{"A", "B", "C", "Unused"} {
		address, err := store.CreateAddress(context.Background(), tripID, name)
		require.NoError(t, err)
		members[i] = *address
	}
	base := resolverWithChain(store)
	ctx := resolverContext(store, counts)
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
		srv.ServeHTTP(w, r.WithContext(db.WithTripDataLoader(r.Context(), db.NewTripDataLoader(counts.Wrap(store)))))
	}))
	for _, selection := range []string{
		"id name",
		"id statistics @skip(if: true) { totalExpense }",
		"id ... on Address @include(if: false) { statistics { totalExpense } }",
	} {
		counts.Reset()
		var response any
		require.NoError(t, c.Post(fmt.Sprintf(`{ trip(tripId:%q) { addresses { %s } } }`, tripID, selection), &response))
		require.Zero(t, factory.calls.Load())
		require.Zero(t, counts.Snapshot().TripRecords.Batches)
	}
	type addressResult struct {
		ID    string
		Stats model.AddressStatistics
		Again model.AddressStatistics
	}
	var live []addressResult
	for _, history := range []bool{false, true} {
		factory.calls.Store(0)
		counts.Reset()
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
		require.Equal(t, int64(1), counts.Snapshot().TripRecords.Batches)
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
	address, err := store.CreateAddress(context.Background(), id, "member")
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
	unused, err := store.CreateAddress(context.Background(), uuid.MustParse(id), "Unused")
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

func (s *recordQueryDB) CheckTripAccess(context.Context, uuid.UUID) error { return nil }

func (s *statisticsCountingFactory) CheckTripAccess(context.Context, uuid.UUID) error { return nil }

func profileNamePtr(s string) *string { return &s }
func (t *resolverTrip) CreateAddressWithOptions(ctx context.Context, name *string, useNickname *bool, bind bool) (*domain.Address, error) {
	a, e := t.CreateAddress(ctx, *name)
	return a, e
}
func (t *resolverTrip) PatchAddress(ctx context.Context, id uuid.UUID, name *string, useNickname *bool) (*domain.Address, bool, error) {
	a, e := t.UpdateAddress(ctx, id, *name)
	return a, true, e
}

func (f *resolverTripFactory) PatchTripInfo(ctx context.Context, id uuid.UUID, name *string, private *bool) (*domain.TripInfo, error) {
	f.selectedID = id
	return f.trip.UpdateInfo(ctx, *name)
}

type bindingRecordQueue struct {
	mq.TripRecordMessageQueue
	ready  chan struct{}
	events chan mq.TripRecordMessage
}

func (q *bindingRecordQueue) Subscribe(uuid.UUID) (uuid.UUID, <-chan mq.TripRecordMessage, error) {
	close(q.ready)
	return uuid.New(), q.events, nil
}
func (q *bindingRecordQueue) DeSubscribe(uuid.UUID) error { return nil }

type bindingAddressQueue struct {
	mq.TripAddressMessageQueue
	ready  chan struct{}
	events chan mq.TripAddressMessage
}

func (q *bindingAddressQueue) Subscribe(uuid.UUID) (uuid.UUID, <-chan mq.TripAddressMessage, error) {
	close(q.ready)
	return uuid.New(), q.events, nil
}
func (q *bindingAddressQueue) DeSubscribe(uuid.UUID) error { return nil }

type bindingQueues struct {
	r *bindingRecordQueue
	a *bindingAddressQueue
}

func (q bindingQueues) GetTripRecordMessageQueue(mq.Action) mq.TripRecordMessageQueue   { return q.r }
func (q bindingQueues) GetTripAddressMessageQueue(mq.Action) mq.TripAddressMessageQueue { return q.a }
func assertClosedBindingStream[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case _, ok := <-ch:
		require.False(t, ok, "revoked stream leaked an event")
	case <-time.After(time.Second):
		t.Fatal("revoked stream did not close")
	}
}

func TestAllSubscriptionsRecheckAccess(t *testing.T) {
	for _, name := range []string{"recordCreate", "recordUpdate", "addressCreate", "addressUpdate", "addressDelete"} {
		for _, departure := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/private", true: "/departure"}[departure], func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				store, users := mem.NewInMemoryDB()
				u, err := users.UpsertUser(ctx, "A")
				require.NoError(t, err)
				v, err := users.UpsertUser(ctx, "B")
				require.NoError(t, err)
				owner, member := domain.WithUserID(ctx, u.ID), domain.WithUserID(ctx, v.ID)
				id := uuid.New()
				require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: id, Name: "stream"}))
				require.NoError(t, store.Bind(owner, id, nil))
				actor := ctx
				if departure {
					require.NoError(t, store.Bind(member, id, nil))
					_, err = store.SetTripPrivate(owner, id, true)
					require.NoError(t, err)
					actor = member
				}
				queues := bindingQueues{&bindingRecordQueue{ready: make(chan struct{}), events: make(chan mq.TripRecordMessage, 1)}, &bindingAddressQueue{ready: make(chan struct{}), events: make(chan mq.TripAddressMessage, 1)}}
				r := (&Resolver{TripFactory: tripservice.NewTripFactory(store, users, nil), TripMessageQueueWrapper: queues}).Subscription()
				var records <-chan *model.RecordSnapshot
				var addresses <-chan *model.AddressSnapshot
				switch name {
				case "recordCreate":
					records, err = r.SubRecordCreate(actor, id.String())
				case "recordUpdate":
					records, err = r.SubRecordUpdate(actor, id.String())
				case "addressCreate":
					addresses, err = r.SubAddressCreate(actor, id.String())
				case "addressUpdate":
					addresses, err = r.SubAddressUpdate(actor, id.String())
				case "addressDelete":
					addresses, err = r.SubAddressDelete(actor, id.String())
				}
				require.NoError(t, err)
				ready := queues.a.ready
				if records != nil {
					ready = queues.r.ready
				}
				select {
				case <-ready:
				case <-time.After(time.Second):
					t.Fatal("subscription not ready")
				}
				if departure {
					require.NoError(t, store.Unbind(member, id))
				} else {
					_, err = store.SetTripPrivate(owner, id, true)
					require.NoError(t, err)
				}
				if records != nil {
					queues.r.events <- mq.TripRecordMessage{TripID: id, ID: uuid.New(), Name: "secret", Category: 0}
					assertClosedBindingStream(t, records)
				} else {
					queues.a.events <- mq.TripAddressMessage{TripID: id, Address: domain.Address{ID: uuid.New(), Name: "secret"}}
					assertClosedBindingStream(t, addresses)
				}
				// Every subscription also rejects a new unauthorized connection.
				for _, subscribe := range []func(context.Context, string) (<-chan *model.RecordSnapshot, error){r.SubRecordCreate, r.SubRecordUpdate} {
					_, err := subscribe(ctx, id.String())
					require.Equal(t, utils.UserError("UNAUTHENTICATED"), err)
				}
				for _, subscribe := range []func(context.Context, string) (<-chan *model.AddressSnapshot, error){r.SubAddressCreate, r.SubAddressUpdate, r.SubAddressDelete} {
					_, err := subscribe(member, id.String())
					require.Equal(t, utils.UserError("FORBIDDEN"), err)
				}
			})
		}
	}
}

func TestGraphQLHistoryAliasesAndCompletePayloads(t *testing.T) {
	counts := new(testutil.LoaderCounters)
	store := mem.NewInMemoryTripDBWrapper()
	id := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: id, Name: "history"}))
	payer, err := store.CreateAddress(context.Background(), id, "payer")
	require.NoError(t, err)
	member, err := store.CreateAddress(context.Background(), id, "member")
	require.NoError(t, err)
	ctx := resolverContext(store, counts)
	base := resolverWithChain(store)
	factory := base.RecordFactory
	trip := base.TripFactory.ForTrip(id)
	value := domain.Record{RecordInfo: domain.RecordInfo{Name: "meal", Amount: 20, PrePayAddress: *payer}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member}}}}
	intent, err := factory.New(ctx, id, value)
	require.NoError(t, err)
	root, err := trip.Append(ctx, intent)
	require.NoError(t, err)
	patch, err := recordpatch.Diff(domain.RecordFields{}, domain.RecordFields{Name: "updated"})
	require.NoError(t, err)
	intent, err = factory.Update(ctx, root.Record.ID(), patch)
	require.NoError(t, err)
	tail, err := trip.Append(ctx, intent)
	require.NoError(t, err)
	value.ID = uuid.New()
	value.IsDeleted = true
	_, err = store.AppendNew(ctx, id, value, seedRecordPolicy{})
	require.NoError(t, err)
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: base}))
	c := client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(db.WithTripDataLoader(r.Context(), db.NewTripDataLoader(counts.Wrap(store)))))
	}))
	counts.Reset()
	var metadata struct{ Trip struct{ Name string } }
	require.NoError(t, c.Post(fmt.Sprintf(`{ trip(tripId: %q) { name } }`, id), &metadata))
	assert.Zero(t, counts.Snapshot().TripRecords.Batches)
	var response struct {
		Latest struct {
			Records []struct {
				ID                           string
				IsDeleted, IsValid, IsActive bool
				ShouldPayAddress             []struct{ ID string }
				ExtendPayMsg                 []float64
			}
			IsValid    bool
			MoneyShare []model.Tx
		}
		History struct {
			Records []struct {
				ID                  string
				IsDeleted, IsActive bool
			}
			IsValid    bool
			MoneyShare []model.Tx
		}
	}
	require.NoError(t, c.Post(fmt.Sprintf(`{
  latest: trip(tripId: %q) {
   records { id isDeleted isValid isActive shouldPayAddress { id } extendPayMsg }
   isValid moneyShare { input { amount address { id name } } output { amount address { id name } } }
  }
  history: trip(tripId: %q, haveHistory: true) {
   records { id isDeleted isActive }
   isValid moneyShare { input { amount address { id name } } output { amount address { id name } } }
  }
 }`, id, id), &response))
	require.Len(t, response.Latest.Records, 1)
	assert.Equal(t, tail.Record.ID().String(), response.Latest.Records[0].ID)
	assert.True(t, response.Latest.Records[0].IsValid)
	assert.True(t, response.Latest.Records[0].IsActive)
	require.Len(t, response.Latest.Records[0].ShouldPayAddress, 1)
	assert.Equal(t, member.ID.String(), response.Latest.Records[0].ShouldPayAddress[0].ID)
	assert.Equal(t, []float64{0}, response.Latest.Records[0].ExtendPayMsg)
	require.Len(t, response.History.Records, 3)
	assert.False(t, response.History.Records[0].IsActive)
	assert.True(t, response.History.Records[2].IsDeleted)
	assert.True(t, response.Latest.IsValid)
	assert.True(t, response.History.IsValid)
	assert.Equal(t, response.Latest.MoneyShare, response.History.MoneyShare)
	assert.Equal(t, int64(2), counts.Snapshot().TripRecords.Batches)
	assert.Zero(t, counts.Snapshot().Records.Batches, "nested fields must not reload individual records")
	// Full payloads no longer require any request reader.
	records, err := base.TripFactory.ForTrip(id).List(resolverContext(store, counts))
	require.NoError(t, err)
	valid, err := records[0].Validate()
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestNameValidationOnEveryMutation(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "旅行 ✈️"}))
	payer, err := database.CreateAddress(context.Background(), tripID, "成員 👩🏽‍💻")
	require.NoError(t, err)
	recordID := uuid.New()
	_, err = database.AppendNew(context.Background(), tripID, domain.Record{
		RecordInfo: domain.RecordInfo{ID: recordID, Name: "<meal> 🍱", Amount: 20, PrePayAddress: *payer},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *payer}}},
	}, seedRecordPolicy{})
	require.NoError(t, err)
	resolver := &mutationResolver{Resolver: resolverWithChain(database)}
	old := model.NewRecord{Name: "<meal> 🍱", Amount: 20, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{payer.ID.String()}}
	for _, tc := range []struct {
		name, value, reason string
		kind                error
	}{
		{"invalid UTF-8", "\xff", "contains invalid characters: invalid UTF-8", textvalidate.ErrInvalidUTF8},
		{"control", "\n", "contains invalid characters: U+000A is not allowed", textvalidate.ErrInvalidCharacter},
		{"line separator", "\u2028", "contains invalid characters: U+2028 is not allowed", textvalidate.ErrInvalidCharacter},
		{"paragraph separator", "\u2029", "contains invalid characters: U+2029 is not allowed", textvalidate.ErrInvalidCharacter},
		{"empty", "", "must not be empty or whitespace-only", textvalidate.ErrEmptyName},
		{"whitespace", " \u3000", "must not be empty or whitespace-only", textvalidate.ErrEmptyName},
		{"too long", strings.Repeat("🍜", 101), "must not exceed 100 Unicode code points", textvalidate.ErrNameTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := old
			next.Name = tc.value
			ctx := resolverContext(database)
			operations := []struct {
				name, field string
				run         func() error
			}{
				{"create trip", "trip", func() error { _, err := resolver.CreateTrip(ctx, model.NewTrip{Name: tc.value}); return err }},
				{"create member trip", "trip", func() error {
					_, err := resolver.CreateTrip(domain.WithUserID(ctx, uuid.New()), model.NewTrip{Name: tc.value})
					return err
				}},
				{"update trip", "trip", func() error {
					_, err := resolver.UpdateTrip(ctx, tripID.String(), model.EditTrip{Name: profileNamePtr(tc.value)})
					return err
				}},
				{"create address", "address", func() error {
					_, err := resolver.CreateAddress(ctx, tripID.String(), model.NewAddress{Name: profileNamePtr(tc.value)}, false)
					return err
				}},
				{"update address", "address", func() error {
					_, err := resolver.UpdateAddress(ctx, tripID.String(), payer.ID.String(), model.NewAddress{Name: profileNamePtr(tc.value)})
					return err
				}},
				{"create record", "record", func() error { _, err := resolver.CreateRecord(ctx, tripID.String(), next); return err }},
				{"update record", "record", func() error {
					_, err := resolver.UpdateRecord(ctx, recordID.String(), model.EditRecord{Old: &old, New: &next})
					return err
				}},
			}
			for _, operation := range operations {
				t.Run(operation.name, func(t *testing.T) {
					err := operation.run()
					require.ErrorIs(t, err, tc.kind)
					if operation.name == "create trip" || operation.name == "create member trip" {
						require.EqualError(t, err, "trip name "+tc.reason)
					}
					require.True(t, strings.HasSuffix(err.Error(), operation.field+" name "+tc.reason), err.Error())
					if operation.field == "record" {
						require.ErrorIs(t, err, tripservice.ErrInvalidRecordSnapshot)
					}
				})
			}
		})
	}
	records, err := database.DataLoaderGetTripRecords(context.Background(), []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, records[tripID], 1)
	require.Equal(t, "<meal> 🍱", records[tripID][0].Name)
}

func TestProfileMutationEventsAndCache(t *testing.T) {
	base := context.Background()
	store, users := mem.NewInMemoryDB()
	u, err := users.UpsertUser(base, "profile", "a@example.com")
	require.NoError(t, err)
	service, err := auth.NewService(base, config.Auth{}, users)
	require.NoError(t, err)
	defer service.Close()
	loader := db.NewTripDataLoader(store)
	ctx := db.WithTripDataLoader(domain.WithUserID(base, u.ID), loader)
	tid := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tid, Name: "profile"}))
	readers := func(context.Context) (db.Reader, error) { return loader, nil }
	queue := &trackingMQ{}
	resolver := &Resolver{Auth: service, TripFactory: tripservice.NewTripFactory(store, users, readers), RecordFactory: tripservice.NewRecordFactory(readers), TripMessageQueueWrapper: queue}
	m := resolver.Mutation()
	_, err = m.UpdateMe(ctx, model.UpdateMe{Name: profileNamePtr("小明")})
	require.NoError(t, err)
	_, err = loader.LoadTripAddresses(ctx, tid)
	require.NoError(t, err)
	a, err := m.CreateAddress(ctx, tid.String(), model.NewAddress{Name: profileNamePtr("原名")}, true)
	require.NoError(t, err)
	require.Equal(t, "原名", a.Name)
	require.False(t, a.UseNickname)
	require.Len(t, queue.addressQueues[mq.ActionCreate].messages, 1)
	event, skip, err := utils.TripAddressMQ2GQL(queue.addressQueues[mq.ActionCreate].messages[0])
	require.NoError(t, err)
	require.False(t, skip)
	require.Equal(t, a, event)
	_, err = m.CreateAddress(ctx, tid.String(), model.NewAddress{Name: profileNamePtr("another")}, true)
	require.Error(t, err)
	require.Len(t, queue.addressQueues[mq.ActionCreate].messages, 1)
	list, err := loader.LoadTripAddresses(ctx, tid)
	require.NoError(t, err)
	require.Len(t, list, 1)
	_, err = m.UpdateAddress(ctx, tid.String(), a.ID, model.NewAddress{})
	require.NoError(t, err)
	require.Empty(t, queue.addressQueues[mq.ActionUpdate].messages)
	yes := true
	_, err = m.UpdateAddress(ctx, tid.String(), a.ID, model.NewAddress{UseNickname: &yes})
	require.NoError(t, err)
	require.Len(t, queue.addressQueues[mq.ActionUpdate].messages, 1)
	event, _, err = utils.TripAddressMQ2GQL(queue.addressQueues[mq.ActionUpdate].messages[0])
	require.NoError(t, err)
	require.Equal(t, "原名", event.Name)
	require.True(t, event.UseNickname)
	no := false
	_, err = m.UpdateAddress(ctx, tid.String(), a.ID, model.NewAddress{UseNickname: &no})
	require.NoError(t, err)
	category := model.RecordCategoryNormal
	rec, err := m.CreateRecord(ctx, tid.String(), model.NewRecord{Name: "meal", Amount: 20, PrePayAddressID: a.ID, ShouldPayAddressIds: []string{a.ID}, Category: &category})
	require.NoError(t, err)
	require.Equal(t, "原名", rec.PrePayAddress.Name)
	revent, _, err := utils.TripRecordMQ2GQL(queue.queues[mq.ActionCreate].messages[0])
	require.NoError(t, err)
	require.Equal(t, rec.PrePayAddress, revent.PrePayAddress)
	_, err = loader.LoadTripRecords(ctx, tid, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	_, err = m.UpdateMe(ctx, model.UpdateMe{Name: profileNamePtr("新名")})
	require.NoError(t, err)
	records, err := loader.LoadTripRecords(ctx, tid, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Equal(t, "原名", records[0].PrePayAddress.Name)
	require.Equal(t, "原名", records[0].ShouldPayAddress[0].Address.Name)
	require.Equal(t, 20., records[0].Amount)
	require.Len(t, queue.addressQueues[mq.ActionUpdate].messages, 2)
}

func TestSharePatchInvalidMergeCanBeQueriedAndRepaired(t *testing.T) {
	for _, scenario := range []string{"no recipients", "unbalanced fixed amounts"} {
		t.Run(scenario, func(t *testing.T) {
			store := mem.NewInMemoryTripDBWrapper()
			tripID := uuid.New()
			require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "repair"}))
			a, err := store.CreateAddress(context.Background(), tripID, "A")
			require.NoError(t, err)
			b, err := store.CreateAddress(context.Background(), tripID, "B")
			require.NoError(t, err)
			resolver := resolverWithChain(store)
			resolver.TripMessageQueueWrapper = &trackingMQ{}
			mutation := &mutationResolver{Resolver: resolver}
			category := model.RecordCategoryNormal
			if scenario == "unbalanced fixed amounts" {
				category = model.RecordCategoryFix
			}
			millis := "1234"
			baseline := model.NewRecord{Name: "meal", Amount: 20, Time: &millis, PrePayAddressID: a.ID.String(), ShouldPayAddressIds: []string{a.ID.String(), b.ID.String()}, ExtendPayMsg: []float64{10, 10}, Category: &category}
			created, err := mutation.CreateRecord(resolverContext(store), tripID.String(), baseline)
			require.NoError(t, err)
			require.True(t, created.IsValid)
			left, right := baseline, baseline
			expected := map[string]float64{}
			if scenario == "no recipients" {
				left.ShouldPayAddressIds = []string{a.ID.String()}
				left.ExtendPayMsg = []float64{10}
				right.ShouldPayAddressIds = []string{b.ID.String()}
				right.ExtendPayMsg = []float64{10}
			} else {
				left.Amount = 30
				left.ExtendPayMsg = []float64{20, 10}
				right.Amount = 40
				right.ExtendPayMsg = []float64{10, 30}
				expected = map[string]float64{a.ID.String(): 20, b.ID.String(): 30}
			}
			first, err := mutation.UpdateRecord(resolverContext(store), created.ID, model.EditRecord{Old: &baseline, New: &left})
			require.NoError(t, err)
			require.True(t, first.IsValid)
			second, err := mutation.UpdateRecord(resolverContext(store), created.ID, model.EditRecord{Old: &baseline, New: &right})
			require.NoError(t, err)
			assert.False(t, second.IsValid, "merge must succeed even when the result needs repair")
			require.NotNil(t, second.ParentRecordID)
			assert.Equal(t, first.ID, *second.ParentRecordID)
			query := &tripResolver{Resolver: resolver}
			records, err := query.Records(resolverContext(store), &model.Trip{ID: tripID.String()})
			require.NoError(t, err)
			require.Len(t, records, 1)
			current := records[0]
			assert.Equal(t, second.ID, current.ID)
			assert.False(t, current.IsValid)
			require.Len(t, current.ExtendPayMsg, len(current.ShouldPayAddress))
			actual := map[string]float64{}
			for i, address := range current.ShouldPayAddress {
				actual[address.ID] = current.ExtendPayMsg[i]
			}
			assert.Equal(t, expected, actual)
			// Build the repair baseline from the actual query payload, as a client does.
			repairOld := right
			repairOld.ShouldPayAddressIds = make([]string, len(current.ShouldPayAddress))
			repairOld.ExtendPayMsg = append([]float64(nil), current.ExtendPayMsg...)
			for i, address := range current.ShouldPayAddress {
				repairOld.ShouldPayAddressIds[i] = address.ID
			}
			repairNew := repairOld
			if scenario == "no recipients" {
				repairNew.ShouldPayAddressIds = []string{a.ID.String()}
				repairNew.ExtendPayMsg = []float64{10}
			} else {
				repairNew.Amount = 50
			}
			repaired, err := mutation.UpdateRecord(resolverContext(store), current.ID, model.EditRecord{Old: &repairOld, New: &repairNew})
			require.NoError(t, err)
			assert.True(t, repaired.IsValid)
			records, err = query.Records(resolverContext(store), &model.Trip{ID: tripID.String()})
			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, repaired.ID, records[0].ID)
			assert.True(t, records[0].IsValid)
			history, err := query.Records(resolverContext(store), &model.Trip{ID: tripID.String(), HaveHistory: true})
			require.NoError(t, err)
			assert.Len(t, history, 4)
		})
	}
}

type unsortedShareQueryDB struct {
	db.TripDBWrapper
	snapshots []db.RecordSnapshot
}

func (s *unsortedShareQueryDB) DataLoaderGetTripRecords(_ context.Context, ids []uuid.UUID, options db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	result := make(map[uuid.UUID][]db.RecordSnapshot)
	for _, id := range ids {
		for _, snapshot := range s.snapshots {
			if snapshot.TripID == id && (options.HaveHistory || snapshot.ChildRecordID == nil) {
				result[id] = append(result[id], snapshot)
			}
		}
	}
	return result, nil
}

func TestShareQuerySortsCurrentAndHistoricalRecordsWithoutMutatingSource(t *testing.T) {
	tripID := uuid.New()
	a := domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Name: "A"}
	b := domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Name: "B"}
	root := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "meal", Amount: 30, Time: time.UnixMilli(1234), PrePayAddress: a}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: b, ExtendMsg: 20}, {Address: a, ExtendMsg: 10}}}}
	tail := root
	tail.ID = uuid.New()
	tail.ParentRecordID = &root.ID
	root.ChildRecordID = &tail.ID
	tail.ShouldPayAddress = []domain.ExtendAddress{{Address: b, ExtendMsg: 22}, {Address: a, ExtendMsg: 11}}
	store := &unsortedShareQueryDB{snapshots: []db.RecordSnapshot{{TripID: tripID, Record: root}, {TripID: tripID, Record: tail}}}
	resolver := &tripResolver{Resolver: resolverWithChain(store)}
	for _, history := range []bool{false, true} {
		name := "current"
		if history {
			name = "history"
		}
		t.Run(name, func(t *testing.T) {
			records, err := resolver.Records(resolverContext(store), &model.Trip{ID: tripID.String(), HaveHistory: history})
			require.NoError(t, err)
			count := 1
			if history {
				count = 2
			}
			require.Len(t, records, count)
			for _, record := range records {
				require.Len(t, record.ShouldPayAddress, 2)
				assert.Equal(t, []*model.Address{{ID: a.ID.String(), Name: "A"}, {ID: b.ID.String(), Name: "B"}}, record.ShouldPayAddress)
				want := []float64{10, 20}
				if record.ID == tail.ID.String() {
					want = []float64{11, 22}
				}
				assert.Equal(t, want, record.ExtendPayMsg)
			}
			records[0].ShouldPayAddress[0].Name = "caller mutation"
			records[0].ExtendPayMsg[0] = 99
			assert.Equal(t, []domain.ExtendAddress{{Address: b, ExtendMsg: 20}, {Address: a, ExtendMsg: 10}}, store.snapshots[0].ShouldPayAddress)
			assert.Equal(t, []domain.ExtendAddress{{Address: b, ExtendMsg: 22}, {Address: a, ExtendMsg: 11}}, store.snapshots[1].ShouldPayAddress)
		})
	}
}

func (s *unsortedShareQueryDB) CheckTripAccess(context.Context, uuid.UUID) error { return nil }

type userFieldAuth struct {
	auth.Service
	userID  uuid.UUID
	trips   []domain.SubscribedTrip
	reads   int
	writes  []string
	readErr error
}

func (s *userFieldAuth) SubscribedTrips(_ context.Context, id uuid.UUID) ([]domain.SubscribedTrip, error) {
	if id != s.userID {
		return nil, errors.New("wrong user")
	}
	s.reads++
	return s.trips, s.readErr
}
func (s *userFieldAuth) SubscribeTrip(_ context.Context, userID, tripID uuid.UUID) error {
	if userID != s.userID {
		return errors.New("wrong user")
	}
	s.writes = append(s.writes, "subscribeTrip")
	s.trips = []domain.SubscribedTrip{{ID: tripID, Name: "saved", LastViewedAt: time.UnixMilli(1234)}}
	return nil
}
func (s *userFieldAuth) UnsubscribeTrip(_ context.Context, userID, _ uuid.UUID) error {
	if userID != s.userID {
		return errors.New("wrong user")
	}
	s.writes = append(s.writes, "unsubscribeTrip")
	s.trips = nil
	return nil
}
func (s *userFieldAuth) TouchSubscribedTrip(_ context.Context, userID, tripID uuid.UUID) error {
	if userID != s.userID {
		return errors.New("wrong user")
	}
	s.writes = append(s.writes, "touchSubscribedTrip")
	s.trips = []domain.SubscribedTrip{{ID: tripID, Name: "touched", LastViewedAt: time.UnixMilli(5678)}}
	return nil
}

func userFieldRequest(t *testing.T, service *userFieldAuth, authenticated bool, query string) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/query", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if authenticated {
		req = req.WithContext(domain.WithUserID(req.Context(), service.userID))
	}
	recorder := httptest.NewRecorder()
	handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: &Resolver{Auth: service}})).ServeHTTP(recorder, req)
	var result map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	return result
}

func TestUserFieldsLoadOnlyWhenSelected(t *testing.T) {
	tripID := uuid.NewString()
	for _, operation := range []string{"me", "subscribeTrip", "unsubscribeTrip", "touchSubscribedTrip"} {
		t.Run(operation, func(t *testing.T) {
			for _, selection := range []string{"id", "id subscribedTrips @skip(if: true) { id }", "id subscribedTrips @include(if: false) { id }", "id ... on User @skip(if: true) { subscribedTrips { id } }", "id ...Details"} {
				service := &userFieldAuth{userID: uuid.New()}
				query := fmt.Sprintf("{ me { %s } }", selection)
				if operation != "me" {
					query = fmt.Sprintf("mutation { %s(tripId:%q) { %s } }", operation, tripID, selection)
				}
				selected := selection == "id ...Details"
				if selected {
					query += " fragment Details on User { subscribedTrips { id name lastViewedAt } }"
				}
				result := userFieldRequest(t, service, true, query)
				require.Nil(t, result["errors"])
				user := result["data"].(map[string]any)[operation].(map[string]any)
				require.Equal(t, service.userID.String(), user["id"])
				if selected {
					require.Equal(t, 1, service.reads)
					trips := user["subscribedTrips"].([]any)
					if operation == "me" || operation == "unsubscribeTrip" {
						require.Empty(t, trips)
					} else {
						require.Len(t, trips, 1)
						expectedName, expectedTime := "saved", "1234"
						if operation == "touchSubscribedTrip" {
							expectedName, expectedTime = "touched", "5678"
						}
						require.Equal(t, map[string]any{"id": tripID, "name": expectedName, "lastViewedAt": expectedTime}, trips[0])
					}
				} else {
					require.Zero(t, service.reads)
				}
				if operation != "me" {
					require.Equal(t, []string{operation}, service.writes)
				}
			}
		})
	}
}

func TestUserFieldReadFailureAndAuthentication(t *testing.T) {
	service := &userFieldAuth{userID: uuid.New(), readErr: errors.New("storage")}
	result := userFieldRequest(t, service, true, "{ me { id } }")
	require.Nil(t, result["errors"])
	require.Zero(t, service.reads)
	result = userFieldRequest(t, service, true, "{ me { subscribedTrips { id } } }")
	require.Nil(t, result["data"].(map[string]any)["me"])
	gqlErr := result["errors"].([]any)[0].(map[string]any)
	require.Equal(t, []any{"me", "subscribedTrips"}, gqlErr["path"])
	require.Equal(t, "INTERNAL_SERVER_ERROR", gqlErr["extensions"].(map[string]any)["code"])
	result = userFieldRequest(t, service, false, "{ me { subscribedTrips { id } } }")
	require.Nil(t, result["errors"])
	require.Nil(t, result["data"].(map[string]any)["me"])
	require.Equal(t, 1, service.reads)
	result = userFieldRequest(t, service, true, fmt.Sprintf("mutation { subscribeTrip(tripId:%q) { subscribedTrips { id } } }", uuid.NewString()))
	require.Nil(t, result["data"])
	gqlErr = result["errors"].([]any)[0].(map[string]any)
	require.Equal(t, []any{"subscribeTrip", "subscribedTrips"}, gqlErr["path"])
	require.Equal(t, []string{"subscribeTrip"}, service.writes)
}

func (s *userFieldAuth) GetUser(_ context.Context, id uuid.UUID) (domain.User, error) {
	return domain.User{ID: id}, nil
}

type bindingReadCounter struct {
	db.TripDBWrapper
	calls int
}

func (s *bindingReadCounter) DataLoaderGetTripUsers(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]db.TripUsers, error) {
	s.calls++
	return s.TripDBWrapper.DataLoaderGetTripUsers(ctx, ids)
}
func TestBoundUsersShareLoaderAndKeepRawNames(t *testing.T) {
	base := context.Background()
	store, users := mem.NewInMemoryDB()
	u, err := users.UpsertUser(base, "bound-user")
	require.NoError(t, err)
	name := "Profile"
	_, err = users.UpdateUser(base, u.ID, &name, nil)
	require.NoError(t, err)
	other, err := users.UpsertUser(base, "unclaimed-user")
	require.NoError(t, err)
	tid := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tid, Name: "T"}))
	a, err := store.CreateAddress(base, tid, "Nickname")
	require.NoError(t, err)
	require.NoError(t, store.Bind(domain.WithUserID(base, u.ID), tid, &a.ID))
	require.NoError(t, store.Bind(domain.WithUserID(base, other.ID), tid, nil))
	counter := &bindingReadCounter{TripDBWrapper: store}
	resolver := resolverWithChain(counter)
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: resolver}))
	c := client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(db.WithTripDataLoader(r.Context(), db.NewTripDataLoader(counter))))
	}))
	var minimal struct{ Trip struct{ ID string } }
	require.NoError(t, c.Post(fmt.Sprintf(`{ trip(tripId:%q) { id } }`, tid.String()), &minimal))
	require.Zero(t, counter.calls)
	var out struct {
		Trip struct {
			Users []struct {
				UserID   string
				UserName *string
			}
			Addresses []struct {
				Name string
				Bind *struct {
					UserID   string
					UserName *string
				}
			}
		}
	}
	require.NoError(t, c.Post(fmt.Sprintf(`{ trip(tripId:%q) { users { userId userName } addresses { name bind { userId userName } } } }`, tid.String()), &out))
	require.Len(t, out.Trip.Users, 2)
	require.Equal(t, "Nickname", out.Trip.Addresses[0].Name)
	require.Equal(t, u.ID.String(), out.Trip.Addresses[0].Bind.UserID)
	require.Equal(t, "Profile", *out.Trip.Addresses[0].Bind.UserName)
	require.Equal(t, 1, counter.calls)
}
