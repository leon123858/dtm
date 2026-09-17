package utils

import (
	"dtm/graph/model"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTimestampParsingBoundaries(t *testing.T) {
	for _, value := range []int64{0, -1, 1234, 1700000000123} {
		text := fmt.Sprint(value)
		parsed, err := ParseJSTimestampString(text)
		require.NoError(t, err)
		require.Equal(t, value, parsed.UnixMilli())
	}
	for _, value := range []string{"", "1.5", "abc", "9223372036854775808"} {
		_, err := ParseJSTimestampString(value)
		require.Error(t, err)
	}
}

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
