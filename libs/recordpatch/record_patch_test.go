package recordpatch

import (
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
	require.Len(t, patch.Changes, 8)
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
	patch.Changes[0].From = float64(100)
	patch.Changes[0].To = float64(100)
	assert.Equal(t, old.EditableFields().ShouldPayAddress[0], copy.Changes[0].From)
	merged, err := Apply(old, copy)
	require.NoError(t, err)
	assert.Equal(t, float64(2), merged.ShouldPayAddress[0].ExtendMsg)
	merged.ShouldPayAddress[0].ExtendMsg = 200
	assert.Equal(t, domain.RecordShare{AddressID: old.ShouldPayAddress[0].Address.ID.String(), ExtendMsg: 2}, copy.Changes[0].To)
}

func recordWithThreeShares() domain.Record {
	record := patchRecord()
	record.ShouldPayAddress = []domain.ExtendAddress{
		{Address: domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Name: "A"}, ExtendMsg: 1},
		{Address: domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Name: "B"}, ExtendMsg: 2},
		{Address: domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), Name: "C"}, ExtendMsg: 3},
	}
	return record
}

// Member edits must identify addresses rather than replacing the collection.
func TestRecordPatchDiffIdentifiesChangedShareAddresses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates map[int]float64
	}{
		{name: "B only", updates: map[int]float64{1: 20}},
		{name: "A and C", updates: map[int]float64{0: 10, 2: 30}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := recordWithThreeShares()
			before, next := old.EditableFields(), old.EditableFields()
			for index, value := range tc.updates {
				next.ShouldPayAddress[index].ExtendMsg = value
			}
			patch, err := Diff(before, next)
			require.NoError(t, err)
			assert.Equal(t, old.EditableFields(), before, "Diff must preserve the baseline")
			assert.Len(t, patch.Changes, len(tc.updates), "each changed member needs its own change")

			// Do not prescribe the complete path layout, but require the member's
			// identity and complete member values so a stale patch can target that member.
			seen := make(map[string]bool)
			for _, change := range patch.Changes {
				assert.Equal(t, odiff.UPDATE, change.Type)
				assert.Contains(t, change.Path, "ShouldPayAddress")
				assert.Len(t, change.Path, 2)
				matched := false
				for index, value := range tc.updates {
					share := before.ShouldPayAddress[index]
					for _, part := range change.Path {
						if part != share.AddressID {
							continue
						}
						assert.False(t, seen[share.AddressID], "duplicate change for %s", share.AddressID)
						seen[share.AddressID] = true
						matched = true
						assert.Equal(t, share, change.From)
						assert.Equal(t, domain.RecordShare{AddressID: share.AddressID, ExtendMsg: value}, change.To)
					}
				}
				assert.True(t, matched, "change must identify an edited AddressID: %v", change.Path)
			}
			assert.Len(t, seen, len(tc.updates), "every edited AddressID must be identified")
			for index, share := range next.ShouldPayAddress {
				expected := before.ShouldPayAddress[index].ExtendMsg
				if value, ok := tc.updates[index]; ok {
					expected = value
				}
				assert.Equal(t, expected, share.ExtendMsg, "Diff must preserve the edited snapshot")
			}
		})
	}
}

func TestRecordPatchInterleavedShareUpdatesPreserveDifferentAddresses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates map[int]float64
	}{
		{name: "A and B", updates: map[int]float64{0: 10}},
		{name: "A+C and B", updates: map[int]float64{0: 10, 2: 30}},
	} {
		for _, reverse := range []bool{false, true} {
			order := "B last"
			if reverse {
				order = "B first"
			}
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				old := recordWithThreeShares()
				before := old.EditableFields()
				left, right := old.EditableFields(), old.EditableFields()
				expected := old.EditableFields()
				for index, value := range tc.updates {
					left.ShouldPayAddress[index].ExtendMsg = value
					expected.ShouldPayAddress[index].ExtendMsg = value
				}
				right.ShouldPayAddress[1].ExtendMsg = 20
				expected.ShouldPayAddress[1].ExtendMsg = 20
				// Both clients edited the same baseline before either update landed.
				first, err := Diff(before, left)
				require.NoError(t, err)
				second, err := Diff(before, right)
				require.NoError(t, err)
				firstExpected := left
				if reverse {
					first, second = second, first
					firstExpected = right
				}
				tail, err := Apply(old, first)
				require.NoError(t, err)
				require.Equal(t, firstExpected, tail.EditableFields())
				merged, err := Apply(tail, second)
				require.NoError(t, err)
				assert.Equal(t, expected, merged.EditableFields(), "applying to the latest tail must retain edits to other AddressIDs")
				assert.Equal(t, before, old.EditableFields(), "Apply must preserve the original record")
				assert.Equal(t, firstExpected, tail.EditableFields(), "Apply must preserve the input tail")
			})
		}
	}
}

// shareSnapshot gives each table row an independent snapshot. Labels identify
// members; comparison below intentionally ignores their positions.
func shareSnapshot(base domain.Record, labels string, values ...float64) domain.RecordFields {
	fields := base.EditableFields()
	fields.ShouldPayAddress = make(domain.RecordShares, len(labels))
	for i, label := range labels {
		id := uuid.UUID{}
		id[15] = byte(label - 'A' + 1)
		fields.ShouldPayAddress[i] = domain.RecordShare{AddressID: id.String(), ExtendMsg: values[i]}
	}
	return fields
}

func TestRecordPatchShareConflictPolicy(t *testing.T) {
	base := recordWithThreeShares()
	snapshot := func(labels string, values ...float64) domain.RecordFields {
		return shareSnapshot(base, labels, values...)
	}
	before := snapshot("ABC", 1, 2, 3)
	for _, tc := range []struct {
		name              string
		left, right, want domain.RecordFields
		reverseWant       *domain.RecordFields
	}{
		{name: "add different IDs", left: snapshot("ABCD", 1, 2, 3, 4), right: snapshot("ABCE", 1, 2, 3, 5), want: snapshot("ABCDE", 1, 2, 3, 4, 5)},
		{name: "add and update", left: snapshot("ABCD", 1, 2, 3, 4), right: snapshot("ABC", 10, 2, 3), want: snapshot("ABCD", 10, 2, 3, 4)},
		{name: "add and delete", left: snapshot("ABCD", 1, 2, 3, 4), right: snapshot("BC", 2, 3), want: snapshot("BCD", 2, 3, 4)},
		{name: "delete and update different IDs", left: snapshot("BC", 2, 3), right: snapshot("ABC", 1, 20, 3), want: snapshot("BC", 20, 3)},
		{name: "delete different IDs", left: snapshot("BC", 2, 3), right: snapshot("AC", 1, 3), want: snapshot("C", 3)},
		{name: "duplicate delete", left: snapshot("BC", 2, 3), right: snapshot("BC", 2, 3), want: snapshot("BC", 2, 3)},
		{name: "delete and update same ID", left: snapshot("BC", 2, 3), right: snapshot("ABC", 10, 2, 3), want: snapshot("ABC", 10, 2, 3), reverseWant: func() *domain.RecordFields { v := snapshot("BC", 2, 3); return &v }()},
		{name: "same update", left: snapshot("ABC", 10, 2, 3), right: snapshot("ABC", 10, 2, 3), want: snapshot("ABC", 10, 2, 3)},
		{name: "later update wins", left: snapshot("ABC", 10, 2, 3), right: snapshot("ABC", 11, 2, 3), want: snapshot("ABC", 11, 2, 3), reverseWant: func() *domain.RecordFields { v := snapshot("ABC", 10, 2, 3); return &v }()},
		{name: "same addition", left: snapshot("ABCD", 1, 2, 3, 4), right: snapshot("ABCD", 1, 2, 3, 4), want: snapshot("ABCD", 1, 2, 3, 4)},
		{name: "later addition wins", left: snapshot("ABCD", 1, 2, 3, 4), right: snapshot("ABCD", 1, 2, 3, 40), want: snapshot("ABCD", 1, 2, 3, 40), reverseWant: func() *domain.RecordFields { v := snapshot("ABCD", 1, 2, 3, 4); return &v }()},
	} {
		for _, reverse := range []bool{false, true} {
			order := "left_then_right"
			if reverse {
				order = "right_then_left"
			}
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				first, err := Diff(before, tc.left)
				require.NoError(t, err)
				second, err := Diff(before, tc.right)
				require.NoError(t, err)
				want := tc.want
				firstWant := tc.left
				if reverse {
					first, second = second, first
					firstWant = tc.right
					if tc.reverseWant != nil {
						want = *tc.reverseWant
					}
				}
				firstCopy, secondCopy := first.Clone(), second.Clone()
				tail, err := Apply(base, first)
				require.NoError(t, err)
				require.ElementsMatch(t, firstWant.ShouldPayAddress, tail.EditableFields().ShouldPayAddress)
				tailBefore := tail.EditableFields()
				merged, err := Apply(tail, second)
				require.NoError(t, err)
				assert.ElementsMatch(t, want.ShouldPayAddress, merged.EditableFields().ShouldPayAddress)
				assert.Equal(t, before, base.EditableFields())
				assert.Equal(t, tailBefore, tail.EditableFields())
				assert.Equal(t, firstCopy, first)
				assert.Equal(t, secondCopy, second)
			})
		}
	}
}

func TestRecordPatchMixedShareOperationsRestoreDeletedMember(t *testing.T) {
	base := recordWithThreeShares()
	before := base.EditableFields()
	removeA := shareSnapshot(base, "BC", 2, 3)
	deletion, err := Diff(before, removeA)
	require.NoError(t, err)
	tail, err := Apply(base, deletion)
	require.NoError(t, err)
	tailBefore := tail.EditableFields()
	// A's stale edit restores A, B changes, C is deleted, D is added, and Name changes.
	next := shareSnapshot(base, "ABD", 10, 20, 4)
	next.Name = "dinner"
	patch, err := Diff(before, next)
	require.NoError(t, err)
	merged, err := Apply(tail, patch)
	require.NoError(t, err)
	assert.ElementsMatch(t, shareSnapshot(base, "ABD", 10, 20, 4).ShouldPayAddress, merged.EditableFields().ShouldPayAddress)
	assert.Equal(t, "dinner", merged.Name)
	assert.Equal(t, tailBefore, tail.EditableFields())
	// A genuine add, based on the snapshot without A, can reintroduce it.
	restored := shareSnapshot(base, "ABC", 99, 2, 3)
	addition, err := Diff(removeA, restored)
	require.NoError(t, err)
	merged, err = Apply(tail, addition)
	require.NoError(t, err)
	assert.ElementsMatch(t, restored.ShouldPayAddress, merged.EditableFields().ShouldPayAddress)
}

func TestRecordPatchShareOrderIsNotAnEdit(t *testing.T) {
	base := recordWithThreeShares()
	before := base.EditableFields()
	for _, labels := range []string{"ABC", "CAB", "BCA", "CBA"} {
		t.Run(labels, func(t *testing.T) {
			values := make([]float64, len(labels))
			for i, label := range labels {
				values[i] = float64(label - 'A' + 1)
			}
			reordered := shareSnapshot(base, labels, values...)
			patch, err := Diff(before, reordered)
			require.NoError(t, err)
			assert.Empty(t, patch.Changes, "a permutation is not an edit")
			changed := shareSnapshot(base, labels, values...)
			for i := range changed.ShouldPayAddress {
				if changed.ShouldPayAddress[i].AddressID == before.ShouldPayAddress[1].AddressID {
					changed.ShouldPayAddress[i].ExtendMsg = 20
				}
			}
			canonical := shareSnapshot(base, "ABC", 1, 20, 3)
			expectedPatch, err := Diff(before, canonical)
			require.NoError(t, err)
			actualPatch, err := Diff(reordered, changed)
			require.NoError(t, err)
			assert.ElementsMatch(t, expectedPatch.Changes, actualPatch.Changes, "patch operations must not depend on slice positions")
			reorderedPatch, err := Diff(before, changed)
			require.NoError(t, err)
			assert.ElementsMatch(t, expectedPatch.Changes, reorderedPatch.Changes, "reordering plus editing must only change B")
			// The latest tail has a different order and an independent edit to A.
			tail := recordWithThreeShares()
			tail.ShouldPayAddress = []domain.ExtendAddress{tail.ShouldPayAddress[2], tail.ShouldPayAddress[0], tail.ShouldPayAddress[1]}
			tail.ShouldPayAddress[1].ExtendMsg = 10
			merged, err := Apply(tail, reorderedPatch)
			require.NoError(t, err)
			assert.ElementsMatch(t, shareSnapshot(base, "ABC", 10, 20, 3).ShouldPayAddress, merged.EditableFields().ShouldPayAddress)
		})
	}
}

func TestRecordPatchMembershipDiffIdentifiesAddress(t *testing.T) {
	base := recordWithThreeShares()
	for _, tc := range []struct {
		name string
		next domain.RecordFields
		kind string
		id   string
	}{
		{"add", shareSnapshot(base, "ABCD", 1, 2, 3, 4), odiff.CREATE, "00000000-0000-0000-0000-000000000004"},
		{"delete", shareSnapshot(base, "AC", 1, 3), odiff.DELETE, "00000000-0000-0000-0000-000000000002"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patch, err := Diff(base.EditableFields(), tc.next)
			require.NoError(t, err)
			require.NotEmpty(t, patch.Changes)
			for _, change := range patch.Changes {
				assert.Equal(t, tc.kind, change.Type)
				assert.Contains(t, change.Path, "ShouldPayAddress")
				assert.Contains(t, change.Path, tc.id, "membership change must target the AddressID, not a list index")
			}
			merged, err := Apply(base, patch)
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.next.ShouldPayAddress, merged.EditableFields().ShouldPayAddress)
		})
	}
}

func TestRecordPatchMalformedOldMemberCanBeRepaired(t *testing.T) {
	tail := patchRecord()
	before, next := tail.EditableFields(), tail.EditableFields()
	before.ShouldPayAddress = domain.RecordShares{{AddressID: "broken"}}
	next.ShouldPayAddress[0].ExtendMsg = 20
	patch, err := Diff(before, next)
	require.NoError(t, err)
	merged, err := Apply(tail, patch)
	require.NoError(t, err)
	assert.Equal(t, next, merged.EditableFields())
}
