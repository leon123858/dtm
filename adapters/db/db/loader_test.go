package db_test

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/adapters/db/testutil"
	"errors"
	"fmt"
	"sync"
	"testing"

	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vikstrous/dataloadgen"
)

type loaderStore struct {
	records map[uuid.UUID]db.RecordSnapshot
}

func (s *loaderStore) DataLoaderGetRecordList(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]db.RecordSnapshot, error) {
	result := make(map[uuid.UUID]db.RecordSnapshot, len(ids))
	for _, id := range ids {
		if record, ok := s.records[id]; ok {
			result[id] = record
		}
	}
	return result, nil
}

func (*loaderStore) DataLoaderGetTripRecords(context.Context, []uuid.UUID, db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	return map[uuid.UUID][]db.RecordSnapshot{}, nil
}
func (*loaderStore) DataLoaderGetTripAddressList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	return map[uuid.UUID][]domain.Address{}, nil
}
func (*loaderStore) DataLoaderGetTripInfoList(context.Context, []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	return map[uuid.UUID]*domain.TripInfo{}, nil
}

func TestTripDataLoaderImplementsReaderAndCachesUntilReset(t *testing.T) {
	counts := new(testutil.LoaderCounters)
	tripID, rootID, childID := uuid.New(), uuid.New(), uuid.New()
	store := &loaderStore{records: map[uuid.UUID]db.RecordSnapshot{
		rootID:  {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: rootID, ChildRecordID: &childID}}},
		childID: {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: childID, ParentRecordID: &rootID}}},
	}}
	counts.Reset()
	reader := db.NewTripDataLoader(counts.Wrap(store))

	_, err := reader.LoadRecord(context.Background(), childID)
	require.NoError(t, err)
	first := counts.Snapshot()
	assert.Equal(t, testutil.DataLoadCount{Batches: 1, Keys: 1}, first.Records)

	_, err = reader.LoadRecord(context.Background(), childID)
	require.NoError(t, err)
	cached := counts.Snapshot()
	assert.Equal(t, first.Records, cached.Records, "the second traversal must be served entirely from cache")

	reader.Reset()
	_, err = reader.LoadRecord(context.Background(), childID)
	require.NoError(t, err)
	invalidated := counts.Snapshot()
	assert.Equal(t, testutil.DataLoadCount{Batches: 2, Keys: 2}, invalidated.Records)
	t.Logf("DataLoader backing fetches: first=%+v cached=%+v invalidated=%+v", first.Records, cached.Records, invalidated.Records)
}

func TestTripDataLoaderResetDropsEveryCache(t *testing.T) {
	counts := new(testutil.LoaderCounters)
	id := uuid.New()
	store := &loaderStore{records: map[uuid.UUID]db.RecordSnapshot{id: {Record: domain.Record{RecordInfo: domain.RecordInfo{ID: id}}}}}
	loader := db.NewTripDataLoader(counts.Wrap(store))
	ctx := context.Background()
	loadEveryCache := func() {
		_, _ = loader.LoadRecord(ctx, id)
		_, _ = loader.LoadTripRecords(ctx, id, db.RecordReadOptions{})
		_, _ = loader.LoadTripAddresses(ctx, id)
		_, _ = loader.LoadTrip(ctx, id)
	}
	assertCounts := func(t *testing.T, want int64) {
		t.Helper()
		got := counts.Snapshot()
		for name, count := range map[string]testutil.DataLoadCount{
			"records": got.Records, "trip records": got.TripRecords, "trip addresses": got.TripAddresses,
			"trips": got.Trips,
		} {
			assert.Equal(t, testutil.DataLoadCount{Batches: want, Keys: want}, count, name)
		}
	}

	counts.Reset()
	loadEveryCache()
	assertCounts(t, 1)
	loadEveryCache()
	assertCounts(t, 1)
	loader.Reset()
	loadEveryCache()
	assertCounts(t, 2)
}

func TestTripDataLoaderContext(t *testing.T) {
	counts := new(testutil.LoaderCounters)
	_, err := db.TripDataLoaderFromContext(context.Background())
	require.EqualError(t, err, "data loader is not available")

	loader := db.NewTripDataLoader(counts.Wrap(&loaderStore{}))
	ctx := db.WithTripDataLoader(context.Background(), loader)
	actual, err := db.TripDataLoaderFromContext(ctx)
	require.NoError(t, err)
	assert.Same(t, loader, actual)
}

// Errors must survive mapped-loader handling even when a store returns no row
// for the requested key (dataloadgen otherwise substitutes ErrNotFound).
type errorLoaderStore struct {
	loaderStore
	err     error
	partial bool
}

func (s *errorLoaderStore) DataLoaderGetRecordList(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]db.RecordSnapshot, error) {
	if s.partial {
		return map[uuid.UUID]db.RecordSnapshot{uuid.New(): {}}, s.err
	}
	return nil, s.err
}
func (s *errorLoaderStore) DataLoaderGetTripRecords(context.Context, []uuid.UUID, db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	if s.partial {
		return map[uuid.UUID][]db.RecordSnapshot{uuid.New(): {}}, s.err
	}
	return nil, s.err
}
func (s *errorLoaderStore) DataLoaderGetTripAddressList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	if s.partial {
		return map[uuid.UUID][]domain.Address{uuid.New(): {}}, s.err
	}
	return nil, s.err
}
func (s *errorLoaderStore) DataLoaderGetTripInfoList(context.Context, []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	if s.partial {
		return map[uuid.UUID]*domain.TripInfo{uuid.New(): {}}, s.err
	}
	return nil, s.err
}

func TestTripDataLoaderPreservesFetchErrors(t *testing.T) {
	t.Parallel()
	for _, want := range []error{domain.ErrUnauthenticated, domain.ErrForbidden, context.Canceled, errors.New("storage failure")} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/partial=%t", want, partial), func(t *testing.T) {
				loader := db.NewTripDataLoader(&errorLoaderStore{err: want, partial: partial})
				ctx, id := context.Background(), uuid.New()
				_, err := loader.LoadTrip(ctx, id)
				require.ErrorIs(t, err, want)
				_, err = loader.LoadRecord(ctx, id)
				require.ErrorIs(t, err, want)
				_, err = loader.LoadTripAddresses(ctx, id)
				require.ErrorIs(t, err, want)
				for _, history := range []bool{false, true} {
					_, err = loader.LoadTripRecords(ctx, id, db.RecordReadOptions{HaveHistory: history})
					require.ErrorIs(t, err, want)
				}
			})
		}
	}
}

func TestTripDataLoaderRequestCachesAndCountersAreIndependent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	store := &loaderStore{records: map[uuid.UUID]db.RecordSnapshot{id: {Record: domain.Record{RecordInfo: domain.RecordInfo{ID: id}}}}}
	firstCounts, secondCounts := new(testutil.LoaderCounters), new(testutil.LoaderCounters)
	first := db.NewTripDataLoader(firstCounts.Wrap(store))
	second := db.NewTripDataLoader(secondCounts.Wrap(store))
	_, err := first.LoadRecord(context.Background(), id)
	require.NoError(t, err)
	require.Zero(t, secondCounts.Snapshot().Records.Batches)
	_, err = second.LoadRecord(context.Background(), id)
	require.NoError(t, err)
	firstCounts.Reset()
	second.Reset()
	_, err = first.LoadRecord(context.Background(), id)
	require.NoError(t, err)
	require.Zero(t, firstCounts.Snapshot().Records.Batches, "resetting counters does not invalidate loader cache")
	_, err = second.LoadRecord(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, testutil.DataLoadCount{Batches: 2, Keys: 2}, secondCounts.Snapshot().Records)
}

func TestTripDataLoaderMissingRecordKeepsNotFound(t *testing.T) {
	t.Parallel()
	loader := db.NewTripDataLoader(&loaderStore{})
	_, err := loader.LoadRecord(context.Background(), uuid.New())
	require.ErrorIs(t, err, dataloadgen.ErrNotFound)
}

type snapshotStore struct {
	loaderStore
	mu    sync.Mutex
	lists []struct {
		ids     []uuid.UUID
		options db.RecordReadOptions
	}
	singles [][]uuid.UUID
}

func (s *snapshotStore) DataLoaderGetTripRecords(_ context.Context, ids []uuid.UUID, options db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists = append(s.lists, struct {
		ids     []uuid.UUID
		options db.RecordReadOptions
	}{append([]uuid.UUID(nil), ids...), options})
	result := make(map[uuid.UUID][]db.RecordSnapshot)
	for _, id := range ids {
		result[id] = []db.RecordSnapshot{}
		for _, record := range s.records {
			if record.TripID == id && (options.HaveHistory || record.ChildRecordID == nil && !record.IsDeleted) {
				result[id] = append(result[id], record)
			}
		}
	}
	return result, nil
}
func (s *snapshotStore) DataLoaderGetRecordList(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]db.RecordSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.singles = append(s.singles, append([]uuid.UUID(nil), ids...))
	return s.loaderStore.DataLoaderGetRecordList(ctx, ids)
}

func TestRecordLoaderModesPrimingAndReset(t *testing.T) {
	tripID, rootID, tailID, deletedID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &snapshotStore{loaderStore: loaderStore{records: map[uuid.UUID]db.RecordSnapshot{
		rootID:    {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: rootID, ChildRecordID: &tailID}}},
		tailID:    {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: tailID, ParentRecordID: &rootID}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: domain.Address{ID: uuid.New()}, ExtendMsg: 12}}}}},
		deletedID: {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: deletedID, IsDeleted: true}}},
	}}}
	ctx := context.Background()
	loader := db.NewTripDataLoader(store)
	// A single record must not prime the trip's entire list.
	_, err := loader.LoadRecord(ctx, tailID)
	require.NoError(t, err)
	fast, err := loader.LoadTripRecords(ctx, tripID, db.RecordReadOptions{})
	require.NoError(t, err)
	require.Len(t, fast, 1)
	history, err := loader.LoadTripRecords(ctx, tripID, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, history, 3)
	root, err := loader.LoadRecord(ctx, rootID)
	require.NoError(t, err)
	assert.Equal(t, &tailID, root.ChildRecordID)
	tail, err := loader.LoadRecord(ctx, tailID)
	require.NoError(t, err)
	assert.Equal(t, fast[0].ShouldPayAddress, tail.ShouldPayAddress)
	require.Len(t, store.singles, 1, "history must prime single record cache")
	for _, options := range []db.RecordReadOptions{{}, {HaveHistory: true}} {
		_, err := loader.LoadTripRecords(ctx, tripID, options)
		require.NoError(t, err)
	}
	require.Len(t, store.lists, 2)
	assert.False(t, store.lists[0].options.HaveHistory)
	assert.True(t, store.lists[1].options.HaveHistory)
	loader.Reset()
	_, err = loader.LoadTripRecords(ctx, tripID, db.RecordReadOptions{})
	require.NoError(t, err)
	_, err = loader.LoadTripRecords(ctx, tripID, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, store.lists, 4)
}

func TestRecordLoaderBatchesTripKeysAndReusesPartialCache(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	store := &snapshotStore{loaderStore: loaderStore{records: map[uuid.UUID]db.RecordSnapshot{}}}
	loader := db.NewTripDataLoader(store)
	ctx := context.Background()
	var workers sync.WaitGroup
	for _, id := range []uuid.UUID{first, second, first} {
		workers.Add(1)
		go func(id uuid.UUID) {
			defer workers.Done()
			values, err := loader.LoadTripRecords(ctx, id, db.RecordReadOptions{})
			assert.NoError(t, err)
			assert.Empty(t, values)
		}(id)
	}
	workers.Wait()
	var fetched []uuid.UUID
	for _, call := range store.lists {
		fetched = append(fetched, call.ids...)
	}
	assert.ElementsMatch(t, []uuid.UUID{first, second}, fetched)
	previousBatches := len(store.lists)
	third := uuid.New()
	_, err := loader.LoadTripRecords(ctx, first, db.RecordReadOptions{})
	require.NoError(t, err)
	_, err = loader.LoadTripRecords(ctx, third, db.RecordReadOptions{})
	require.NoError(t, err)
	require.Len(t, store.lists, previousBatches+1)
	assert.Equal(t, []uuid.UUID{third}, store.lists[previousBatches].ids)
}

func (*loaderStore) DataLoaderGetTripUsers(context.Context, []uuid.UUID) (map[uuid.UUID]db.TripUsers, error) {
	return map[uuid.UUID]db.TripUsers{}, nil
}

func (*errorLoaderStore) DataLoaderGetTripUsers(context.Context, []uuid.UUID) (map[uuid.UUID]db.TripUsers, error) {
	return map[uuid.UUID]db.TripUsers{}, nil
}
