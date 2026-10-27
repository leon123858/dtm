package utils

import (
	"dtm/graph/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNormalizeRecordInputShape(t *testing.T) {
	input := model.NewRecord{ShouldPayAddressIds: []string{"member"}}
	require.True(t, NormalizeRecordRequest(&input))
	require.Equal(t, model.RecordCategoryNormal, *input.Category)
	input.ExtendPayMsg = []float64{1, 2}
	require.False(t, NormalizeRecordRequest(&input))
	input.ExtendPayMsg = nil
	unknown := model.RecordCategory("UNKNOWN")
	input.Category = &unknown
	require.False(t, NormalizeRecordRequest(&input))
}
