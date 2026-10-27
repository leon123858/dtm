package textvalidate

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrBlankText = errors.New("must not be empty or whitespace-only")
var ErrInvalidSubject = errors.New("invalid token subject")

// ValidateNonBlank checks presence without trimming or rewriting the value.
func ValidateNonBlank(value string) error {
	if strings.TrimSpace(value) == "" {
		return ErrBlankText
	}
	return nil
}

// ValidateSubject checks the provider's subject format, not token authenticity.
// Subjects use their own 128-code-point limit rather than display-name rules.
func ValidateSubject(value string) error {
	if ValidateNonBlank(value) != nil || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 {
		return ErrInvalidSubject
	}
	return nil
}
