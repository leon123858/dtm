package mem

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"dtm/chain"
	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTrip(t *testing.T) (*inMemoryTripDBWrapper, uuid.UUID, domain.Address, domain.Address) {
	t.Helper()
	database := NewInMemoryTripDBWrapper().(*inMemoryTripDBWrapper)
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := database.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := database.CreateAddress(tripID, "member")
	require.NoError(t, err)
	return database, tripID, *payer, *member
}

func paymentRecord(id uuid.UUID, payer, member domain.Address) domain.Record {
	return domain.Record{RecordInfo: domain.RecordInfo{ID: id, Name: "meal", Amount: 20, Time: time.Now(), PrePayAddress: payer, Category: domain.CategoryNormal}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: member}}}}
}

func TestAppendPatchUsesCanonicalTailAndSupportsNoOp(t *testing.T) {
	database, tripID, payer, member := setupTrip(t)
	rootID := uuid.New()
	require.NoError(t, database.AppendNew(context.Background(), tripID, paymentRecord(rootID, payer, member)))
	name := "dinner"
	gotTrip, first, appended, err := database.AppendPatch(context.Background(), rootID, domain.RecordPatch{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, tripID, gotTrip)
	assert.True(t, appended)
	assert.Equal(t, rootID, *first.ParentRecordID)

	_, same, appended, err := database.AppendPatch(context.Background(), rootID, domain.RecordPatch{Name: &name})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, first.ID, same.ID)
}

func TestConcurrentPatchesFromHistoricalIDsStayLinear(t *testing.T) {
	database, tripID, payer, member := setupTrip(t)
	rootID := uuid.New()
	require.NoError(t, database.AppendNew(context.Background(), tripID, paymentRecord(rootID, payer, member)))
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func(amount float64) {
			defer group.Done()
			_, _, _, err := database.AppendPatch(context.Background(), rootID, domain.RecordPatch{Amount: &amount})
			require.NoError(t, err)
		}(float64(i + 1))
	}
	group.Wait()
	records := database.tripsData[tripID].Records
	require.Len(t, records, 13)
	seen := map[uuid.UUID]bool{}
	current := records[0]
	for {
		assert.False(t, seen[current.ID])
		seen[current.ID] = true
		if current.ChildRecordID == nil {
			break
		}
		for _, candidate := range records {
			if candidate.ID == *current.ChildRecordID {
				current = candidate
				break
			}
		}
	}
	assert.Len(t, seen, 13)
}

func TestDeleteRestoreAndDeepCopies(t *testing.T) {
	database, tripID, payer, member := setupTrip(t)
	rootID := uuid.New()
	require.NoError(t, database.AppendNew(context.Background(), tripID, paymentRecord(rootID, payer, member)))
	deleted := true
	_, deletion, appended, err := database.AppendPatch(context.Background(), rootID, domain.RecordPatch{IsDeleted: &deleted})
	require.NoError(t, err)
	assert.True(t, appended)
	assert.True(t, deletion.IsDeleted)
	_, _, appended, err = database.AppendPatch(context.Background(), rootID, domain.RecordPatch{IsDeleted: &deleted})
	require.NoError(t, err)
	assert.False(t, appended)
	restored := false
	_, restoration, appended, err := database.AppendPatch(context.Background(), deletion.ID, domain.RecordPatch{IsDeleted: &restored})
	require.NoError(t, err)
	assert.True(t, appended)
	assert.False(t, restoration.IsDeleted)
	*restoration.ParentRecordID = uuid.New()
	assert.Equal(t, deletion.ID, *database.tripsData[tripID].Records[2].ParentRecordID)
}

func TestAppendPatchReturnsTypedTailResolutionErrors(t *testing.T) {
	database, tripID, payer, member := setupTrip(t)
	rootID, canonicalID, branchID := uuid.New(), uuid.New(), uuid.New()
	root := paymentRecord(rootID, payer, member)
	canonical := paymentRecord(canonicalID, payer, member)
	branch := paymentRecord(branchID, payer, member)
	root.ChildRecordID = &canonicalID
	canonical.ParentRecordID = &rootID
	branch.ParentRecordID = &rootID
	database.tripsData[tripID].Records = []domain.Record{root, canonical, branch}

	_, _, _, err := database.AppendPatch(context.Background(), branchID, domain.RecordPatch{})
	var resolution *chain.TailResolutionError
	require.ErrorAs(t, err, &resolution)
	assert.Equal(t, branchID, resolution.TargetID)
	assert.True(t, errors.Is(err, chainlist.ErrNonCanonical))

	missing := uuid.New()
	_, _, _, err = database.AppendPatch(context.Background(), missing, domain.RecordPatch{})
	require.ErrorAs(t, err, &resolution)
	assert.True(t, errors.Is(err, chainlist.ErrNodeNotFound))
}
