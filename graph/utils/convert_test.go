package utils

import (
	"strings"
	"testing"

	"dtm/domain"
	"dtm/graph/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToModelRecordCheckedRejectsUnknownCategory(t *testing.T) {
	_, err := ToModelRecordChecked(domain.RecordInfo{ID: uuid.New(), Category: domain.RecordCategory(99)}, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown RecordCategory")
}

func TestToModelRecordCheckedReturnsNonNullablePaymentPayload(t *testing.T) {
	info := domain.RecordInfo{
		ID: uuid.New(), Category: domain.CategoryNormal, Name: "", Amount: 0,
	}
	record, err := ToModelRecordChecked(info, false, false)
	require.NoError(t, err)
	require.NotNil(t, record.Name)
	assert.Equal(t, "", *record.Name)
	require.NotNil(t, record.Amount)
	assert.Equal(t, float64(0), *record.Amount)
	require.NotNil(t, record.PrePayAddress)
	assert.False(t, record.EventValid)
}

func validPatchInput() model.NewRecord {
	category := model.RecordCategoryNormal
	return model.NewRecord{Name: "meal", Amount: 20, PrePayAddressID: uuid.NewString(), ShouldPayAddressIds: []string{uuid.NewString()}, Category: &category}
}

func changedFields(t *testing.T, old, next model.NewRecord) map[string]any {
	t.Helper()
	patch, err := BuildRecordPatch(old, next)
	require.NoError(t, err)
	fields := map[string]any{}
	for _, change := range patch.Changes {
		require.Len(t, change.Path, 1)
		fields[change.Path[0]] = change.To
	}
	return fields
}

func TestBuildRecordPatchDeletionPresence(t *testing.T) {
	for _, test := range []struct {
		name      string
		old, next *bool
		changed   bool
	}{
		{"omitted", nil, nil, false},
		{"delete", nil, ptr(true), true},
		{"default false", nil, ptr(false), false},
		{"already deleted", ptr(true), ptr(true), false},
		{"inherit deletion", ptr(true), nil, false},
		{"restore", ptr(true), ptr(false), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			old := validPatchInput()
			next := old
			old.IsDeleted, next.IsDeleted = test.old, test.next
			fields := changedFields(t, old, next)
			_, changed := fields["IsDeleted"]
			assert.Equal(t, test.changed, changed)
			if changed {
				assert.Equal(t, *test.next, fields["IsDeleted"])
			}
		})
	}
}

func TestBuildRecordPatchIncludesOnlyChangedFields(t *testing.T) {
	old := validPatchInput()
	next := old
	next.Amount = 30
	next.PrePayAddressID = uuid.NewString()
	next.Category = ptr(model.RecordCategoryFix)
	next.Time = ptr("1234")
	next.ShouldPayAddressIds = []string{uuid.NewString()}
	next.ExtendPayMsg = []float64{30}
	assert.Equal(t, map[string]any{
		"Amount": float64(30), "PrePayAddressID": next.PrePayAddressID,
		"Category": "1", "Time": "1234",
		"ShouldPayAddress": domain.RecordShares{{AddressID: next.ShouldPayAddressIds[0], ExtendMsg: 30}},
	}, changedFields(t, old, next))
}

func TestBuildRecordPatchEquivalentInputs(t *testing.T) {
	old := validPatchInput()
	old.Time = ptr("001234")
	old.Category = nil
	next := old
	next.Time = ptr("+1234")
	next.Category = ptr(model.RecordCategoryNormal)
	next.PrePayAddressID = strings.ToUpper(old.PrePayAddressID)
	next.ShouldPayAddressIds = []string{strings.ToUpper(old.ShouldPayAddressIds[0])}
	for _, amounts := range [][]float64{nil, {}, {0}} {
		next.ExtendPayMsg = amounts
		assert.Empty(t, changedFields(t, old, next))
	}
}

func TestBuildRecordPatchTimePresence(t *testing.T) {
	old := validPatchInput()
	next := old
	assert.Empty(t, changedFields(t, old, next))
	old.Time = ptr("broken old timestamp")
	assert.Empty(t, changedFields(t, old, next))
	next.Time = ptr("0")
	assert.Equal(t, map[string]any{"Time": "0"}, changedFields(t, old, next))
	old.Time = nil
	assert.Equal(t, map[string]any{"Time": "0"}, changedFields(t, old, next))
}

func TestBuildRecordPatchRepairsMalformedBaseline(t *testing.T) {
	next := validPatchInput()
	next.Time = ptr("1234")
	old := next
	old.PrePayAddressID = "broken"
	old.ShouldPayAddressIds = []string{"broken"}
	old.Time = ptr("broken")
	old.Category = ptr(model.RecordCategory("BROKEN"))
	old.Amount = 0
	assert.Len(t, changedFields(t, old, next), 5)
}

func TestBuildRecordPatchRejectsMalformedNew(t *testing.T) {
	old := validPatchInput()
	for _, edit := range []func(*model.NewRecord){
		func(r *model.NewRecord) { r.Time = ptr("bad") },
		func(r *model.NewRecord) { r.PrePayAddressID = "bad" },
		func(r *model.NewRecord) { r.ShouldPayAddressIds = []string{"bad"} },
		func(r *model.NewRecord) { r.Category = ptr(model.RecordCategory("bad")) },
		func(r *model.NewRecord) { r.ExtendPayMsg = []float64{0, 0} },
	} {
		next := old
		edit(&next)
		_, err := BuildRecordPatch(old, next)
		require.Error(t, err)
	}
}

func ptr[T any](value T) *T { return &value }
