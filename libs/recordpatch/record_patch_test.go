package recordpatch

import (
	"strings"
	"testing"
	"time"

	"dtm/domain"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func patchRecord() domain.Record {
	member := domain.Address{ID: uuid.New(), Name: "member"}
	parent := uuid.New()
	return domain.Record{
		RecordInfo: domain.RecordInfo{ID: uuid.New(), ParentRecordID: &parent, Name: "meal", Amount: 20,
			Time: time.Unix(123, 123456789), PrePayAddress: domain.Address{ID: uuid.New(), Name: "payer"}, Category: domain.CategoryNormal},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: member}}},
	}
}

func TestRecordPatchPreservesTailAndReplacesOnlyChangedFields(t *testing.T) {
	old := patchRecord()
	next := old.EditableFields()
	next.Name = "dinner"
	patch, err := Diff(old.EditableFields(), next)
	require.NoError(t, err)
	tail := old
	tail.ID = uuid.New()
	tail.Amount = 50
	merged, err := Apply(tail, patch)
	require.NoError(t, err)
	expected := tail
	expected.Name = "dinner"
	assert.Equal(t, expected, merged)
	assert.Equal(t, "meal", tail.Name)
	assert.Equal(t, tail.Time, merged.Time, "unmodified time retains sub-millisecond precision")

	*merged.ParentRecordID = uuid.New()
	merged.ShouldPayAddress[0].ExtendMsg = 99
	assert.Equal(t, *old.ParentRecordID, *tail.ParentRecordID)
	assert.Zero(t, tail.ShouldPayAddress[0].ExtendMsg)
}

func TestRecordPatchEveryEditableFieldRoundTrips(t *testing.T) {
	old := patchRecord()
	next := old.EditableFields()
	next.Name, next.Amount, next.Time = "updated", 42, "3456"
	next.PrePayAddressID, next.Category, next.IsDeleted = uuid.NewString(), "2", true
	next.ShouldPayAddress = domain.RecordShares{{AddressID: uuid.NewString(), ExtendMsg: 3}}
	patch, err := Diff(old.EditableFields(), next)
	require.NoError(t, err)
	require.Len(t, patch.Changes, 7)
	merged, err := Apply(old, patch)
	require.NoError(t, err)
	assert.Equal(t, next, merged.EditableFields())
	assert.Equal(t, old.ID, merged.ID)
	assert.Equal(t, old.ParentRecordID, merged.ParentRecordID)

	noop, err := Diff(next, next)
	require.NoError(t, err)
	assert.Empty(t, noop.Changes)
	same, err := Apply(merged, noop)
	require.NoError(t, err)
	assert.Equal(t, merged, same)
}

func TestRecordSharesAreAtomicOnDivergedTail(t *testing.T) {
	old := patchRecord()
	a, b := uuid.NewString(), uuid.NewString()
	before := old.EditableFields()
	before.ShouldPayAddress = domain.RecordShares{{AddressID: a, ExtendMsg: 1}, {AddressID: b, ExtendMsg: 2}}
	for _, shares := range []domain.RecordShares{
		{{AddressID: a, ExtendMsg: 2}, {AddressID: b, ExtendMsg: 1}}, // Amount exchange must not disappear as an unordered diff.
		{{AddressID: b, ExtendMsg: 2}, {AddressID: a, ExtendMsg: 1}}, // Order remains significant.
		{{AddressID: a, ExtendMsg: 1}},
		{{AddressID: a, ExtendMsg: 1}, {AddressID: b, ExtendMsg: 2}, {AddressID: uuid.NewString(), ExtendMsg: 3}},
		{},
	} {
		next := before
		next.ShouldPayAddress = shares
		patch, err := Diff(before, next)
		require.NoError(t, err)
		require.Len(t, patch.Changes, 1)
		assert.Equal(t, []string{"ShouldPayAddress"}, patch.Changes[0].Path)
		assert.Equal(t, odiff.UPDATE, patch.Changes[0].Type)
		// Tail has a completely different member and length from the baseline.
		merged, err := Apply(old, patch)
		require.NoError(t, err)
		assert.Equal(t, shares, merged.EditableFields().ShouldPayAddress)
		assert.Equal(t, old.Name, merged.Name)
	}
}

func TestRecordPatchOwnsChangelogValues(t *testing.T) {
	old := patchRecord()
	before := old.EditableFields()
	next := old.EditableFields()
	next.ShouldPayAddress[0].ExtendMsg = 2
	patch, err := Diff(before, next)
	require.NoError(t, err)
	next.ShouldPayAddress[0].ExtendMsg = 99
	before.ShouldPayAddress[0].ExtendMsg = 99
	copy := patch.Clone()
	patch.Changes[0].Path[0] = "bad"
	patch.Changes[0].From.(domain.RecordShares)[0].ExtendMsg = 100
	patch.Changes[0].To.(domain.RecordShares)[0].ExtendMsg = 100
	assert.Zero(t, copy.Changes[0].From.(domain.RecordShares)[0].ExtendMsg)
	merged, err := Apply(old, copy)
	require.NoError(t, err)
	assert.Equal(t, float64(2), merged.ShouldPayAddress[0].ExtendMsg)
	merged.ShouldPayAddress[0].ExtendMsg = 200
	assert.Equal(t, float64(2), copy.Changes[0].To.(domain.RecordShares)[0].ExtendMsg)
}

func TestRecordPatchRejectsInvalidChangesWithoutMutatingTail(t *testing.T) {
	old := patchRecord()
	for _, change := range []odiff.Change{
		{Type: odiff.UPDATE, Path: []string{"ID"}, From: old.ID, To: uuid.New()},
		{Type: odiff.UPDATE, Path: []string{"ParentRecordID"}, From: old.ParentRecordID, To: (*uuid.UUID)(nil)},
		{Type: odiff.UPDATE, Path: []string{"ShouldPayAddress", "0", "ExtendMsg"}, From: float64(0), To: float64(1)},
		{Type: odiff.UPDATE, Path: []string{"Amount"}, From: float64(20), To: "bad"},
		{Type: odiff.DELETE, Path: []string{"Name"}, From: "meal"},
		{Type: odiff.UPDATE, Path: nil, From: "meal", To: "bad"},
		{Type: odiff.UPDATE, Path: []string{"Time"}, From: "123123", To: "bad"},
	} {
		t.Run(change.Type+"/"+strings.Join(change.Path, "."), func(t *testing.T) {
			patch := domain.RecordPatch{Changes: odiff.Changelog{
				{Type: odiff.UPDATE, Path: []string{"Name"}, From: "meal", To: "dinner"}, change,
			}}
			_, err := Apply(old, patch)
			require.Error(t, err)
			assert.Equal(t, "meal", old.Name)
			assert.Zero(t, old.ShouldPayAddress[0].ExtendMsg)
		})
	}
}
