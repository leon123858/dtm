package db

import (
	"context"
	"sync"
	"testing"

	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type snapshotStore struct {
	debugLoaderStore
	mu    sync.Mutex
	lists []struct {
		ids     []uuid.UUID
		options RecordReadOptions
	}
	singles [][]uuid.UUID
}

func (s *snapshotStore) DataLoaderGetTripRecords(_ context.Context, ids []uuid.UUID, options RecordReadOptions) (map[uuid.UUID][]RecordSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists = append(s.lists, struct {
		ids     []uuid.UUID
		options RecordReadOptions
	}{append([]uuid.UUID(nil), ids...), options})
	result := make(map[uuid.UUID][]RecordSnapshot)
	for _, id := range ids {
		result[id] = []RecordSnapshot{}
		for _, record := range s.records {
			if record.TripID == id && (options.HaveHistory || record.ChildRecordID == nil && !record.IsDeleted) {
				result[id] = append(result[id], record)
			}
		}
	}
	return result, nil
}
func (s *snapshotStore) DataLoaderGetRecordList(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]RecordSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.singles = append(s.singles, append([]uuid.UUID(nil), ids...))
	return s.debugLoaderStore.DataLoaderGetRecordList(ctx, ids)
}

func TestRecordLoaderModesPrimingAndReset(t *testing.T) {
	tripID, rootID, tailID, deletedID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &snapshotStore{debugLoaderStore: debugLoaderStore{records: map[uuid.UUID]RecordSnapshot{
		rootID:    {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: rootID, ChildRecordID: &tailID}}},
		tailID:    {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: tailID, ParentRecordID: &rootID}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: domain.Address{ID: uuid.New()}, ExtendMsg: 12}}}}},
		deletedID: {TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: deletedID, IsDeleted: true}}},
	}}}
	ctx := context.Background()
	loader := NewTripDataLoader(store)
	// A single record must not prime the trip's entire list.
	_, err := loader.LoadRecord(ctx, tailID)
	require.NoError(t, err)
	fast, err := loader.LoadTripRecords(ctx, tripID, RecordReadOptions{})
	require.NoError(t, err)
	require.Len(t, fast, 1)
	history, err := loader.LoadTripRecords(ctx, tripID, RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, history, 3)
	root, err := loader.LoadRecord(ctx, rootID)
	require.NoError(t, err)
	assert.Equal(t, &tailID, root.ChildRecordID)
	tail, err := loader.LoadRecord(ctx, tailID)
	require.NoError(t, err)
	assert.Equal(t, fast[0].ShouldPayAddress, tail.ShouldPayAddress)
	require.Len(t, store.singles, 1, "history must prime single record cache")
	for _, options := range []RecordReadOptions{{}, {HaveHistory: true}} {
		_, err := loader.LoadTripRecords(ctx, tripID, options)
		require.NoError(t, err)
	}
	require.Len(t, store.lists, 2)
	assert.False(t, store.lists[0].options.HaveHistory)
	assert.True(t, store.lists[1].options.HaveHistory)
	loader.Reset()
	_, err = loader.LoadTripRecords(ctx, tripID, RecordReadOptions{})
	require.NoError(t, err)
	_, err = loader.LoadTripRecords(ctx, tripID, RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, store.lists, 4)
}

func TestRecordLoaderBatchesTripKeysAndReusesPartialCache(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	store := &snapshotStore{debugLoaderStore: debugLoaderStore{records: map[uuid.UUID]RecordSnapshot{}}}
	loader := NewTripDataLoader(store)
	ctx := context.Background()
	values, err := loader.getTripRecords[0].LoadAll(ctx, []uuid.UUID{first, second, first})
	require.NoError(t, err)
	require.Len(t, values, 3)
	require.Len(t, store.lists, 1)
	assert.ElementsMatch(t, []uuid.UUID{first, second}, store.lists[0].ids)
	third := uuid.New()
	_, err = loader.getTripRecords[0].LoadAll(ctx, []uuid.UUID{first, third})
	require.NoError(t, err)
	require.Len(t, store.lists, 2)
	assert.Equal(t, []uuid.UUID{third}, store.lists[1].ids)
}
