package textvalidate

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTimestampParsingBoundaries(t *testing.T) {
	for _, value := range []int64{0, -1, 1234, 1700000000123} {
		text := fmt.Sprint(value)
		parsed, err := ParseTimestamp(text)
		require.NoError(t, err)
		require.Equal(t, value, parsed.UnixMilli())
	}
	for _, value := range []string{"", "1.5", "abc", "9223372036854775808"} {
		_, err := ParseTimestamp(value)
		require.Error(t, err)
	}
}
