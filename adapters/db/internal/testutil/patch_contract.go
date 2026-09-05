package testutil

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/domain"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CheckPatchContract runs the same changelog/tail contract against both stores.
func CheckPatchContract(t *testing.T, store db.TripDBWrapper) {
	t.Helper()
	ctx := context.Background()
	tripID := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "patch contract"}))
	payer, err := store.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := store.CreateAddress(tripID, "member")
	require.NoError(t, err)
	base := domain.Record{
		RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "meal", Amount: 20, Time: time.UnixMilli(1234), PrePayAddress: *payer},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member}}},
	}
	_, err = store.AppendNew(ctx, tripID, base, Materializer{})
	require.NoError(t, err)
	old := base.EditableFields()
	updated := base.EditableFields()
	updated.Amount, updated.Time, updated.PrePayAddressID = 30, "4567", member.ID.String()
	updated.ShouldPayAddress = domain.RecordShares{{AddressID: payer.ID.String(), ExtendMsg: 5}}
	_, first, appended, err := store.AppendPatch(ctx, base.ID, Patch(t, old, updated), Materializer{})
	require.NoError(t, err)
	require.True(t, appended)

	// A patch derived from the original baseline must inherit the new tail.
	renamed := old
	renamed.Name = "dinner"
	namePatch := Patch(t, old, renamed)
	_, second, appended, err := store.AppendPatch(ctx, base.ID, namePatch, Materializer{})
	require.NoError(t, err)
	require.True(t, appended)
	require.Equal(t, first.ID, *second.ParentRecordID)
	updated.Name = "dinner"
	assert.Equal(t, updated, second.EditableFields())

	_, same, appended, err := store.AppendPatch(ctx, base.ID, namePatch, Materializer{})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, second.ID, same.ID)

	// A real list change replaces the tail list rather than editing old indices.
	listEdit := old
	listEdit.ShouldPayAddress = domain.RecordShares{{AddressID: member.ID.String(), ExtendMsg: 7}, {AddressID: payer.ID.String(), ExtendMsg: 8}}
	_, third, appended, err := store.AppendPatch(ctx, base.ID, Patch(t, old, listEdit), Materializer{})
	require.NoError(t, err)
	require.True(t, appended)
	assert.Equal(t, second.ID, *third.ParentRecordID)
	assert.Equal(t, listEdit.ShouldPayAddress, third.EditableFields().ShouldPayAddress)
	assert.Equal(t, "dinner", third.Name)
	assert.Equal(t, float64(30), third.Amount)

	before, err := store.DataLoaderGetRecordInfoList(ctx, []uuid.UUID{tripID})
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		patch  domain.RecordPatch
		policy Materializer
	}{
		{"invalid path", domain.RecordPatch{Changes: odiff.Changelog{
			{Type: odiff.UPDATE, Path: []string{"Name"}, From: "dinner", To: "partial"},
			{Type: odiff.UPDATE, Path: []string{"ChildRecordID"}, From: "", To: "bad"},
		}}, Materializer{}},
		{"invalid patched value", domain.RecordPatch{Changes: odiff.Changelog{
			{Type: odiff.UPDATE, Path: []string{"Name"}, From: "dinner", To: "partial"},
			{Type: odiff.UPDATE, Path: []string{"Time"}, From: "4567", To: "invalid"},
		}}, Materializer{}},
		{"rejected snapshot", namePatch, Materializer{Err: errors.New("snapshot rejected")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, appended, err := store.AppendPatch(ctx, base.ID, test.patch, test.policy)
			require.Error(t, err)
			assert.False(t, appended)
			after, err := store.DataLoaderGetRecordInfoList(ctx, []uuid.UUID{tripID})
			require.NoError(t, err)
			assert.ElementsMatch(t, before[tripID], after[tripID], "failed patches must leave every record and link unchanged")
		})
	}

	// Independent field updates race from the same old baseline and both survive.
	nameEdit, amountEdit := old, old
	nameEdit.Name, amountEdit.Amount = "concurrent", 60
	patches := []domain.RecordPatch{Patch(t, old, nameEdit), Patch(t, old, amountEdit)}
	var group sync.WaitGroup
	failures := make(chan error, len(patches))
	for _, patch := range patches {
		group.Go(func() {
			_, _, _, err := store.AppendPatch(ctx, base.ID, patch, Materializer{})
			failures <- err
		})
	}
	group.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	records, err := store.DataLoaderGetRecordInfoList(ctx, []uuid.UUID{tripID})
	require.NoError(t, err)
	require.Len(t, records[tripID], 6)
	var tails int
	for _, info := range records[tripID] {
		if info.ChildRecordID == nil {
			tails++
			assert.Equal(t, "concurrent", info.Name)
			assert.Equal(t, float64(60), info.Amount)
		}
	}
	assert.Equal(t, 1, tails)
}
