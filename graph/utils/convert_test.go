package utils

import (
	"testing"
	"time"

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

func TestBuildRecordPatchDeletionPresence(t *testing.T) {
	category := model.RecordCategoryNormal
	oldInput := model.NewRecord{Category: &category}
	newInput := oldInput
	assert.Nil(t, BuildRecordPatch(oldInput, newInput, &domain.Record{}).IsDeleted)

	deleted := true
	newInput.IsDeleted = &deleted
	patch := BuildRecordPatch(oldInput, newInput, &domain.Record{})
	if assert.NotNil(t, patch.IsDeleted) {
		assert.True(t, *patch.IsDeleted)
	}

	oldInput.IsDeleted = &deleted
	assert.Nil(t, BuildRecordPatch(oldInput, newInput, &domain.Record{}).IsDeleted)
	restored := false
	newInput.IsDeleted = &restored
	patch = BuildRecordPatch(oldInput, newInput, &domain.Record{})
	if assert.NotNil(t, patch.IsDeleted) {
		assert.False(t, *patch.IsDeleted)
	}
}

func TestBuildRecordPatchIncludesOnlyChangedFields(t *testing.T) {
	oldCategory := model.RecordCategoryNormal
	newCategory := model.RecordCategoryFix
	oldInput := model.NewRecord{
		Name: "meal", Amount: 20, PrePayAddressID: uuid.NewString(),
		ShouldPayAddressIds: []string{uuid.NewString()}, ExtendPayMsg: []float64{10}, Category: &oldCategory,
	}
	newInput := oldInput
	newInput.Amount = 30
	newInput.PrePayAddressID = uuid.NewString()
	newInput.ShouldPayAddressIds = []string{uuid.NewString()}
	newInput.ExtendPayMsg = []float64{30}
	newInput.Category = &newCategory
	timestamp := "1234"
	newInput.Time = &timestamp

	prePayID := uuid.MustParse(newInput.PrePayAddressID)
	shouldPayID := uuid.MustParse(newInput.ShouldPayAddressIds[0])
	recordTime := time.UnixMilli(1234)
	newRecord := domain.Record{
		RecordInfo: domain.RecordInfo{
			Name: newInput.Name, Amount: newInput.Amount, Time: recordTime,
			PrePayAddress: domain.Address{ID: prePayID}, Category: domain.CategoryFix,
		},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: domain.Address{ID: shouldPayID}, ExtendMsg: 30}}},
	}

	patch := BuildRecordPatch(oldInput, newInput, &newRecord)
	assert.Nil(t, patch.Name)
	if assert.NotNil(t, patch.Amount) {
		assert.Equal(t, float64(30), *patch.Amount)
	}
	if assert.NotNil(t, patch.PrePayAddressID) {
		assert.Equal(t, prePayID, *patch.PrePayAddressID)
	}
	if assert.NotNil(t, patch.Category) {
		assert.Equal(t, domain.CategoryFix, *patch.Category)
	}
	if assert.NotNil(t, patch.Time) {
		assert.Equal(t, recordTime, *patch.Time)
	}
	if assert.NotNil(t, patch.ShouldPayAddress) {
		require.Len(t, *patch.ShouldPayAddress, 1)
		assert.Equal(t, shouldPayID, (*patch.ShouldPayAddress)[0].Address.ID)
		assert.Empty(t, (*patch.ShouldPayAddress)[0].Address.Name, "patch addresses need not be canonicalized in the resolver")
	}
	assert.Nil(t, patch.IsDeleted)
}
