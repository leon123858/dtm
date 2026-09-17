// Package textvalidate validates names without modifying their text.
package textvalidate

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidUTF8      = errors.New("contains invalid characters: invalid UTF-8")
	ErrInvalidCharacter = errors.New("contains invalid characters")
	ErrEmptyName        = errors.New("must not be empty or whitespace-only")
	ErrNameTooLong      = errors.New("must not exceed 100 Unicode code points")
)

// ValidateName allows plain text, including markup and Unicode format characters.
// Length is measured in Unicode code points, not bytes or grapheme clusters.
func ValidateName(value string) error {
	if !utf8.ValidString(value) {
		return ErrInvalidUTF8
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("%w: U+%04X is not allowed", ErrInvalidCharacter, r)
		}
	}
	if strings.TrimSpace(value) == "" {
		return ErrEmptyName
	}
	if utf8.RuneCountInString(value) > 100 {
		return ErrNameTooLong
	}
	return nil
}
