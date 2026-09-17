package textvalidate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateName(t *testing.T) {
	for _, name := range []string{"  原文  ", "<meal><script>", "'\"; DROP TABLE records; --", "!@#$%^&*()[]{}+/\\", "e\u0301", "👩🏽‍💻", "\u200d", "\ufffd", strings.Repeat("中", 100), strings.Repeat("🍜", 100), strings.Repeat("👩🏽‍💻", 25), strings.Repeat("e\u0301", 50), "\u200c\u2060\ufeff"} {
		require.NoError(t, ValidateName(name), "%q", name)
	}
	for _, tt := range []struct {
		value   string
		kind    error
		message string
	}{
		{"\n\xff", ErrInvalidUTF8, "contains invalid characters: invalid UTF-8"},
		{"\x00", ErrInvalidCharacter, "contains invalid characters: U+0000 is not allowed"},
		{"\t", ErrInvalidCharacter, "contains invalid characters: U+0009 is not allowed"},
		{"\n\t", ErrInvalidCharacter, "contains invalid characters: U+000A is not allowed"},
		{"\u007f", ErrInvalidCharacter, "contains invalid characters: U+007F is not allowed"},
		{"\u0085", ErrInvalidCharacter, "contains invalid characters: U+0085 is not allowed"},
		{"\u2028", ErrInvalidCharacter, "contains invalid characters: U+2028 is not allowed"},
		{"\u2029", ErrInvalidCharacter, "contains invalid characters: U+2029 is not allowed"},
		{strings.Repeat("a", 101) + "\n", ErrInvalidCharacter, "contains invalid characters: U+000A is not allowed"},
		{"", ErrEmptyName, ErrEmptyName.Error()},
		{strings.Repeat(" \u3000\u00a0", 101), ErrEmptyName, ErrEmptyName.Error()},
		{strings.Repeat("中", 101), ErrNameTooLong, ErrNameTooLong.Error()},
		{strings.Repeat("👩🏽‍💻", 26), ErrNameTooLong, ErrNameTooLong.Error()},
	} {
		err := ValidateName(tt.value)
		require.ErrorIs(t, err, tt.kind)
		require.EqualError(t, err, tt.message)
	}
}

func TestValidateNameRejectsEveryControlCharacter(t *testing.T) {
	// Unicode Cc consists of C0 (U+0000–001F), DEL, and C1 (U+007F–009F).
	for _, interval := range [][2]rune{{0, 0x1f}, {0x7f, 0x9f}} {
		for r := interval[0]; r <= interval[1]; r++ {
			t.Run(fmt.Sprintf("U+%04X", r), func(t *testing.T) {
				err := ValidateName("before" + string(r) + "after")
				require.ErrorIs(t, err, ErrInvalidCharacter)
				require.EqualError(t, err, fmt.Sprintf("contains invalid characters: U+%04X is not allowed", r))
			})
		}
	}
}

func TestValidateNameRejectsMalformedUTF8BeforeOtherRules(t *testing.T) {
	for _, invalid := range []string{"\xff", "\xc0\xaf", "\xe2\x82", "\xed\xa0\x80", "\xf4\x90\x80\x80"} {
		t.Run(fmt.Sprintf("%x", invalid), func(t *testing.T) {
			require.ErrorIs(t, ValidateName("\n"+strings.Repeat("a", 101)+invalid), ErrInvalidUTF8)
		})
	}
}
