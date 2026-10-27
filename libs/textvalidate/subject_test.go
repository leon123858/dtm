package textvalidate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSubject(t *testing.T) {
	for _, value := range []string{"subject", " original ", strings.Repeat("中", 128)} {
		require.NoError(t, ValidateSubject(value))
	}
	for _, value := range []string{"", " \t", "\xff", strings.Repeat("中", 129)} {
		require.ErrorIs(t, ValidateSubject(value), ErrInvalidSubject)
	}
}

func TestValidateNonBlank(t *testing.T) {
	require.NoError(t, ValidateNonBlank(" original "))
	for _, value := range []string{"", " \t\n", "\u3000"} {
		require.ErrorIs(t, ValidateNonBlank(value), ErrBlankText)
	}
}
