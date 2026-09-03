package db

import (
	"context"
	"testing"

	"dtm/chain"
	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type debugLoaderStore struct {
	records map[uuid.UUID]chain.RecordNode
}

func (s *debugLoaderStore) DataLoaderGetRecordList(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]chain.RecordNode, error) {
	result := make(map[uuid.UUID]chain.RecordNode, len(ids))
	for _, id := range ids {
		if record, ok := s.records[id]; ok {
			result[id] = record
		}
	}
	return result, nil
}

func (*debugLoaderStore) DataLoaderGetRecordInfoList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.RecordInfo, error) {
	return map[uuid.UUID][]domain.RecordInfo{}, nil
}
func (*debugLoaderStore) DataLoaderGetTripAddressList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	return map[uuid.UUID][]domain.Address{}, nil
}
func (*debugLoaderStore) DataLoaderGetRecordShouldPayList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.ExtendAddress, error) {
	return map[uuid.UUID][]domain.ExtendAddress{}, nil
}
func (*debugLoaderStore) DataLoaderGetTripInfoList(context.Context, []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	return map[uuid.UUID]*domain.TripInfo{}, nil
}

func TestRecordChainReaderUsesCacheUntilExplicitInvalidation(t *testing.T) {
	tripID, rootID, childID := uuid.New(), uuid.New(), uuid.New()
	store := &debugLoaderStore{records: map[uuid.UUID]chain.RecordNode{
		rootID:  {TripID: tripID, Info: domain.RecordInfo{ID: rootID, ChildRecordID: &childID}},
		childID: {TripID: tripID, Info: domain.RecordInfo{ID: childID, ParentRecordID: &rootID}},
	}}
	DataLoaderDebug.Reset()
	reader := NewRecordChainReader(NewTripDataLoader(store))
	factory := chain.NewRecordFactory(nil, reader)

	_, err := factory.ByID(context.Background(), childID)
	require.NoError(t, err)
	first := DataLoaderDebug.Snapshot()
	assert.Equal(t, DataLoadCount{Batches: 1, Keys: 1}, first.Records)

	_, err = factory.ByID(context.Background(), childID)
	require.NoError(t, err)
	cached := DataLoaderDebug.Snapshot()
	assert.Equal(t, first.Records, cached.Records, "the second traversal must be served entirely from cache")

	reader.(*recordChainReader).InvalidateRecords(rootID, childID)
	_, err = factory.ByID(context.Background(), childID)
	require.NoError(t, err)
	invalidated := DataLoaderDebug.Snapshot()
	assert.Equal(t, DataLoadCount{Batches: 2, Keys: 2}, invalidated.Records)
	t.Logf("DataLoader backing fetches: first=%+v cached=%+v invalidated=%+v", first.Records, cached.Records, invalidated.Records)
}

func TestTripDataLoaderResetDropsEveryCache(t *testing.T) {
	id := uuid.New()
	store := &debugLoaderStore{records: map[uuid.UUID]chain.RecordNode{id: {Info: domain.RecordInfo{ID: id}}}}
	loader := NewTripDataLoader(store)
	ctx := context.Background()
	loadEveryCache := func() {
		_, _ = loader.GetRecord.Load(ctx, id)
		_, _ = loader.GetRecordInfoList.Load(ctx, id)
		_, _ = loader.GetTripAddressList.Load(ctx, id)
		_, _ = loader.GetRecordShouldPayList.Load(ctx, id)
		_, _ = loader.GetTripInfoList.Load(ctx, id)
	}
	assertCounts := func(t *testing.T, want int64) {
		t.Helper()
		got := DataLoaderDebug.Snapshot()
		for name, count := range map[string]DataLoadCount{
			"records": got.Records, "trip records": got.TripRecords, "trip addresses": got.TripAddresses,
			"record should-pays": got.RecordShouldPays, "trips": got.Trips,
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
