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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CheckPatchContract runs the same changelog/tail contract against both stores.
func CheckPatchContract(t *testing.T, store db.TripDBWrapper) {
	t.Helper()
	t.Run("address constraints", func(t *testing.T) { checkPatchAddressConstraints(t, store) })
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
	_, first, appended, err := store.AppendPatch(ctx, tripID, base.ID, Patch(t, old, updated), Materializer{})
	require.NoError(t, err)
	require.True(t, appended)

	// A patch derived from the original baseline must inherit the new tail.
	renamed := old
	renamed.Name = "dinner"
	namePatch := Patch(t, old, renamed)
	_, second, appended, err := store.AppendPatch(ctx, tripID, base.ID, namePatch, Materializer{})
	require.NoError(t, err)
	require.True(t, appended)
	require.Equal(t, first.ID, *second.ParentRecordID)
	updated.Name = "dinner"
	assert.Equal(t, updated, second.EditableFields())

	_, same, appended, err := store.AppendPatch(ctx, tripID, base.ID, namePatch, Materializer{})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, second.ID, same.ID)

	// A stale member update restores a deleted member; CREATE updates the existing payer.
	listEdit := old
	listEdit.ShouldPayAddress = domain.RecordShares{{AddressID: member.ID.String(), ExtendMsg: 7}, {AddressID: payer.ID.String(), ExtendMsg: 8}}
	_, third, appended, err := store.AppendPatch(ctx, tripID, base.ID, Patch(t, old, listEdit), Materializer{})
	require.NoError(t, err)
	require.True(t, appended)
	assert.Equal(t, second.ID, *third.ParentRecordID)
	assert.ElementsMatch(t, listEdit.ShouldPayAddress, third.EditableFields().ShouldPayAddress)
	assert.Equal(t, "dinner", third.Name)
	assert.Equal(t, float64(30), third.Amount)

	before, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		patch  domain.RecordPatch
		policy Materializer
	}{
		{"materialization failure", Patch(t, domain.RecordFields{Name: "dinner", Time: "4567"}, domain.RecordFields{Name: "partial", Time: "invalid"}), Materializer{}},
		{"rejected snapshot", namePatch, Materializer{Err: errors.New("snapshot rejected")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, appended, err := store.AppendPatch(ctx, tripID, base.ID, test.patch, test.policy)
			require.Error(t, err)
			assert.False(t, appended)
			after, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
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
			_, _, _, err := store.AppendPatch(ctx, tripID, base.ID, patch, Materializer{})
			failures <- err
		})
	}
	group.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	records, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
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

func checkPatchAddressConstraints(t *testing.T, store db.TripDBWrapper) {
	ctx := context.Background()
	tripID, otherTripID := uuid.New(), uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "constraints"}))
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: otherTripID, Name: "other"}))
	member, err := store.CreateAddress(tripID, "member")
	require.NoError(t, err)
	foreign, err := store.CreateAddress(otherTripID, "foreign")
	require.NoError(t, err)
	base := domain.Record{
		RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "meal", Amount: 20, Time: time.UnixMilli(1234), PrePayAddress: *member},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member}}},
	}
	_, err = store.AppendNew(ctx, tripID, base, Materializer{})
	require.NoError(t, err)
	for _, address := range []domain.Address{{ID: uuid.New()}, *foreign} {
		for _, payer := range []bool{false, true} {
			invalid := base
			invalid.ID = uuid.New()
			if payer {
				invalid.PrePayAddress = address
			} else {
				invalid.ShouldPayAddress = []domain.ExtendAddress{{Address: address, ExtendMsg: 7}}
			}
			_, err := store.AppendNew(ctx, tripID, invalid, Materializer{})
			require.Error(t, err, "storage must reject addresses outside the trip even without business validation")
			// The absent share is an UPDATE, exercising the library's reconstruction.
			old := invalid.EditableFields()
			if payer {
				old.PrePayAddressID = member.ID.String()
			} else {
				old.ShouldPayAddress[0].ExtendMsg = 1
			}
			_, _, appended, err := store.AppendPatch(ctx, tripID, base.ID, Patch(t, old, invalid.EditableFields()), Materializer{})
			require.Error(t, err)
			assert.False(t, appended)
			history, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
			require.NoError(t, err)
			require.Len(t, history[tripID], 1)
			assert.Equal(t, base.ID, history[tripID][0].ID)
			assert.Nil(t, history[tripID][0].ParentRecordID)
			assert.Nil(t, history[tripID][0].ChildRecordID)
			assert.Equal(t, base.EditableFields(), history[tripID][0].Record.EditableFields())
		}
	}
}

// CheckSharePatchContract isolates member merging from business validation.
func CheckSharePatchContract(t *testing.T, store db.TripDBWrapper) {
	t.Helper()
	for _, scenario := range []string{"stale member edit", "reorder no-op", "deleted update restores member", "repeated deletion no-op"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			tripID := uuid.New()
			require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "share contract"}))
			a, err := store.CreateAddress(tripID, "A")
			require.NoError(t, err)
			b, err := store.CreateAddress(tripID, "B")
			require.NoError(t, err)
			c, err := store.CreateAddress(tripID, "C")
			require.NoError(t, err)
			base := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "meal", Amount: 20, Time: time.UnixMilli(1234), PrePayAddress: *a}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *a, ExtendMsg: 1}, {Address: *b, ExtendMsg: 2}}}}
			_, err = store.AppendNew(ctx, tripID, base, Materializer{})
			require.NoError(t, err)
			before := base.EditableFields()
			first, second := base.EditableFields(), base.EditableFields()
			first.ShouldPayAddress = append(first.ShouldPayAddress, domain.RecordShare{AddressID: c.ID.String(), ExtendMsg: 3})
			want := append(domain.RecordShares(nil), first.ShouldPayAddress...)
			wantAppend := false
			switch scenario {
			case "stale member edit":
				second.ShouldPayAddress[1].ExtendMsg = 20
				want[1].ExtendMsg = 20
				wantAppend = true
			case "reorder no-op":
				second.ShouldPayAddress[0], second.ShouldPayAddress[1] = second.ShouldPayAddress[1], second.ShouldPayAddress[0]
			case "deleted update restores member":
				first.ShouldPayAddress = first.ShouldPayAddress[1:]
				second.ShouldPayAddress[0].ExtendMsg = 10
				want[0].ExtendMsg = 10
				wantAppend = true
			case "repeated deletion no-op":
				first.ShouldPayAddress = first.ShouldPayAddress[1:]
				second.ShouldPayAddress = second.ShouldPayAddress[1:]
				want = want[1:]
			}
			_, tail, appended, err := store.AppendPatch(ctx, tripID, base.ID, Patch(t, before, first), Materializer{})
			require.NoError(t, err)
			require.True(t, appended)
			_, merged, appended, err := store.AppendPatch(ctx, tripID, base.ID, Patch(t, before, second), Materializer{})
			require.NoError(t, err)
			assert.Equal(t, wantAppend, appended)
			assert.ElementsMatch(t, want, merged.EditableFields().ShouldPayAddress)
			expectedCount := 2
			if wantAppend {
				expectedCount = 3
				require.NotNil(t, merged.ParentRecordID)
				assert.Equal(t, tail.ID, *merged.ParentRecordID)
			} else {
				assert.Equal(t, tail.ID, merged.ID)
			}
			history, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
			require.NoError(t, err)
			assert.Len(t, history[tripID], expectedCount)
			tails := 0
			for _, record := range history[tripID] {
				if record.ChildRecordID == nil {
					tails++
					assert.Equal(t, merged.ID, record.ID)
					assert.ElementsMatch(t, want, record.Record.EditableFields().ShouldPayAddress)
				}
			}
			assert.Equal(t, 1, tails)
		})
	}
}
