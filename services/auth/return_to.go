package auth

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Bound work on unauthenticated input, including nested percent encoding.
const (
	maxReturnToBytes       = 4096
	maxReturnToDecodeDepth = 4
)

var escapedByte = regexp.MustCompile(`%[0-9a-fA-F]{2}`)

// ValidateReturnTo returns only a validated browser-relative destination.
// The caller has already decoded the outer login request's query parameter.
func ValidateReturnTo(raw string) (string, error) {
	if raw == "" {
		return "/", nil
	}
	if len(raw) > maxReturnToBytes {
		return "", ErrRequest
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || strings.HasPrefix(raw, "//") {
		return "", ErrRequest
	}
	// Do not normalize or decode page paths into the allowlist. This excludes
	// encoded separators, dot segments and nested path encoding altogether.
	if u.RawPath != "" || strings.Contains(u.EscapedPath(), "%") {
		return "", ErrRequest
	}
	parts := strings.Split(u.Path, "/")
	switch u.Path {
	case "/", "/history":
	default:
		if len(parts) != 3 || parts[0] != "" || parts[1] != "trip" {
			return "", ErrRequest
		}
		if _, err := uuid.Parse(parts[2]); err != nil {
			return "", ErrRequest
		}
	}
	// Inspect encoded query/fragment values without rewriting them. Repeated
	// decoding catches nested escaped controls and backslashes. A literal '%'
	// produced by a valid escape is allowed; malformed original escapes are not.
	for _, component := range []string{raw, u.RawQuery, u.EscapedFragment()} {
		for depth := 0; ; depth++ {
			if !utf8.ValidString(component) || strings.ContainsFunc(component, func(r rune) bool { return r == '\\' || unicode.IsControl(r) }) {
				return "", ErrRequest
			}
			decoded, err := url.PathUnescape(component)
			if err != nil {
				if depth == 0 {
					return "", ErrRequest
				}
				decoded = escapedByte.ReplaceAllStringFunc(component, func(value string) string { v, _ := url.PathUnescape(value); return v })
			}
			if decoded == component {
				break
			}
			if depth == maxReturnToDecodeDepth {
				return "", ErrRequest
			}
			component = decoded
		}
	}
	return raw, nil
}
