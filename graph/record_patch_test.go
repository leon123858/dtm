package graph

import (
	"testing"

	"dtm/domain"
	"dtm/graph/model"

	"github.com/stretchr/testify/assert"
)

func TestBuildRecordPatchDeletionPresence(t *testing.T) {
	category := model.RecordCategoryNormal
	oldInput := model.NewRecord{Category: &category}
	newInput := oldInput
	assert.Nil(t, buildRecordPatch(oldInput, newInput, &domain.Record{}).IsDeleted)

	deleted := true
	newInput.IsDeleted = &deleted
	patch := buildRecordPatch(oldInput, newInput, &domain.Record{})
	if assert.NotNil(t, patch.IsDeleted) {
		assert.True(t, *patch.IsDeleted)
	}

	oldInput.IsDeleted = &deleted
	assert.Nil(t, buildRecordPatch(oldInput, newInput, &domain.Record{}).IsDeleted)
	restored := false
	newInput.IsDeleted = &restored
	patch = buildRecordPatch(oldInput, newInput, &domain.Record{})
	if assert.NotNil(t, patch.IsDeleted) {
		assert.False(t, *patch.IsDeleted)
	}
}
