package db

import (
	"context"
	"testing"

	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type debugLoaderStore struct {
	records map[uuid.UUID]RecordSnapshot
}

func (s *debugLoaderStore) DataLoaderGetRecordList(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]RecordSnapshot, error) {
	result := make(map[uuid.UUID]RecordSnapshot, len(ids))
	for _, id := range ids {
		if record, ok := s.records[id]; ok {
			result[id] = record
		}
	}
	return result, nil
}

func (*debugLoaderStore) DataLoaderGetTripRecords(context.Context, []uuid.UUID, RecordReadOptions) (map[uuid.UUID][]RecordSnapshot, error) {
	return map[uuid.UUID][]RecordSnapshot{}, nil
}
func (*debugLoaderStore) DataLoaderGetTripAddressList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	return map[uuid.UUID][]domain.Address{}, nil
}
func (*debugLoaderStore) DataLoaderGetTripInfoList(context.Context, []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	return map[uuid.UUID]*domain.TripInfo{}, nil
}

func TestTripDataLoaderImplementsReaderAndCachesUntilReset(t *testing.T) {
	tripID, rootID, childID := uuid.New(), uuid.New(), uuid.New()
	store := &debugLoaderStore{records: map[uuid.UUID]RecordSnapshot{
		rootID:  {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: rootID, ChildRecordID: &childID}}},
		childID: {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: childID, ParentRecordID: &rootID}}},
	}}
	DataLoaderDebug.Reset()
	reader := NewTripDataLoader(store)

	_, err := reader.LoadRecord(context.Background(), childID)
	require.NoError(t, err)
	first := DataLoaderDebug.Snapshot()
	assert.Equal(t, DataLoadCount{Batches: 1, Keys: 1}, first.Records)

	_, err = reader.LoadRecord(context.Background(), childID)
	require.NoError(t, err)
	cached := DataLoaderDebug.Snapshot()
	assert.Equal(t, first.Records, cached.Records, "the second traversal must be served entirely from cache")

	reader.Reset()
	_, err = reader.LoadRecord(context.Background(), childID)
	require.NoError(t, err)
	invalidated := DataLoaderDebug.Snapshot()
	assert.Equal(t, DataLoadCount{Batches: 2, Keys: 2}, invalidated.Records)
	t.Logf("DataLoader backing fetches: first=%+v cached=%+v invalidated=%+v", first.Records, cached.Records, invalidated.Records)
}

func TestTripDataLoaderResetDropsEveryCache(t *testing.T) {
	id := uuid.New()
	store := &debugLoaderStore{records: map[uuid.UUID]RecordSnapshot{id: {Record: domain.Record{RecordInfo: domain.RecordInfo{ID: id}}}}}
	loader := NewTripDataLoader(store)
	ctx := context.Background()
	loadEveryCache := func() {
		_, _ = loader.LoadRecord(ctx, id)
		_, _ = loader.LoadTripRecords(ctx, id, RecordReadOptions{})
		_, _ = loader.LoadTripAddresses(ctx, id)
		_, _ = loader.LoadTrip(ctx, id)
	}
	assertCounts := func(t *testing.T, want int64) {
		t.Helper()
		got := DataLoaderDebug.Snapshot()
		for name, count := range map[string]DataLoadCount{
			"records": got.Records, "trip records": got.TripRecords, "trip addresses": got.TripAddresses,
			"trips": got.Trips,
		} {
			assert.Equal(t, DataLoadCount{Batches: want, Keys: want}, count, name)
		}
	}

	DataLoaderDebug.Reset()
	loadEveryCache()
	assertCounts(t, 1)
	loadEveryCache()
	assertCounts(t, 1)
	loader.Reset()
	loadEveryCache()
	assertCounts(t, 2)
}

func TestTripDataLoaderContext(t *testing.T) {
	_, err := TripDataLoaderFromContext(context.Background())
	require.EqualError(t, err, "data loader is not available")

	loader := NewTripDataLoader(&debugLoaderStore{})
	ctx := WithTripDataLoader(context.Background(), loader)
	actual, err := TripDataLoaderFromContext(ctx)
	require.NoError(t, err)
	assert.Same(t, loader, actual)
}
