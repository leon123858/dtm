package utils

import (
	"testing"

	"dtm/domain"

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
