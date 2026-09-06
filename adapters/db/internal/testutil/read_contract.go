package testutil

import (
	"context"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CheckReadContract exercises full records, history filtering and batched trip isolation.
func CheckReadContract(t *testing.T, store db.TripDBWrapper) {
	t.Helper()
	ctx := context.Background()
	tripID, emptyID := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{tripID, emptyID} {
		require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: id, Name: "read contract"}))
	}
	payer, err := store.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := store.CreateAddress(tripID, "member")
	require.NoError(t, err)
	root := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "root", Amount: 30, Time: time.Unix(1, 0), PrePayAddress: *payer}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *payer, ExtendMsg: 10}, {Address: *member, ExtendMsg: 20}}}}
	root, err = store.AppendNew(ctx, tripID, root, Materializer{})
	require.NoError(t, err)
	_, tail, _, err := store.AppendPatch(ctx, tripID, root.ID, Patch(t, domain.RecordFields{}, domain.RecordFields{Name: "tail"}), Materializer{})
	require.NoError(t, err)
	deleted := root
	deleted.ID = uuid.New()
	deleted.IsDeleted = true
	_, err = store.AppendNew(ctx, tripID, deleted, Materializer{})
	require.NoError(t, err)
	empty := root
	empty.ID = uuid.New()
	empty.ShouldPayAddress = nil
	_, err = store.AppendNew(ctx, tripID, empty, Materializer{})
	require.NoError(t, err)
	for _, history := range []bool{false, true} {
		got, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID, emptyID}, db.RecordReadOptions{HaveHistory: history})
		require.NoError(t, err)
		assert.Empty(t, got[emptyID])
		expected := 2
		if history {
			expected = 4
		}
		require.Len(t, got[tripID], expected)
		byID := map[uuid.UUID]db.RecordSnapshot{}
		for _, record := range got[tripID] {
			byID[record.ID] = record
			assert.Equal(t, tripID, record.TripID)
		}
		assert.Equal(t, *payer, byID[tail.ID].PrePayAddress)
		assert.ElementsMatch(t, tail.ShouldPayAddress, byID[tail.ID].ShouldPayAddress)
		assert.Empty(t, byID[empty.ID].ShouldPayAddress)
		if !history {
			assert.NotContains(t, byID, root.ID)
			assert.NotContains(t, byID, deleted.ID)
		}
	}
	records, err := store.DataLoaderGetRecordList(ctx, []uuid.UUID{root.ID, deleted.ID, tail.ID})
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, &tail.ID, records[root.ID].ChildRecordID)
	assert.True(t, records[deleted.ID].IsDeleted)
	assert.ElementsMatch(t, root.ShouldPayAddress, records[root.ID].ShouldPayAddress)
	missing := uuid.New()
	partial, err := store.DataLoaderGetRecordList(ctx, []uuid.UUID{tail.ID, missing})
	require.Error(t, err)
	assert.Contains(t, partial, tail.ID)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.DataLoaderGetTripRecords(canceled, []uuid.UUID{tripID}, db.RecordReadOptions{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.DataLoaderGetRecordList(canceled, []uuid.UUID{tail.ID})
	require.ErrorIs(t, err, context.Canceled)
}
