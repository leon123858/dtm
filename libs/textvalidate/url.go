package textvalidate

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidURLComponent = errors.New("invalid URL component")
var escapedByte = regexp.MustCompile(`%[0-9a-fA-F]{2}`)
var redirectPathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

// ValidOrigin accepts a bare HTTPS origin, or HTTP when explicitly allowed.
func ValidOrigin(raw string, allowHTTP bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Scheme == "https" || (allowHTTP && u.Scheme == "http")) && u.Path == ""
}

// ValidRedirectPath restricts callbacks to unescaped, cookie-safe path segments.
// Input is checked as supplied, never normalized.
func ValidRedirectPath(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "" && u.Host == "" && u.User == nil &&
		u.Opaque == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		u.EscapedPath() == raw && redirectPathPattern.MatchString(raw) && path.Clean(raw) == raw
}

// ParseUnescapedRelativeURL accepts browser-relative destinations with literal
// paths. Route allowlists and total input-size limits belong to the caller.
func ParseUnescapedRelativeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || strings.HasPrefix(raw, "//") {
		return nil, ErrInvalidURLComponent
	}
	if u.RawPath != "" || strings.Contains(u.EscapedPath(), "%") {
		return nil, ErrInvalidURLComponent
	}
	return u, nil
}

// ValidateEncodedURLComponent rejects invalid UTF-8, controls and backslashes,
// including nested percent escapes. It inspects decoded copies, never rewrites
// the caller's value. A percent produced by a valid escape may remain literal.
// Callers must also bound input size and validate URL structure and destinations.
func ValidateEncodedURLComponent(component string, maxDepth int) error {
	if maxDepth < 0 {
		return ErrInvalidURLComponent
	}
	for depth := 0; ; depth++ {
		if !utf8.ValidString(component) || strings.ContainsFunc(component, func(r rune) bool { return r == '\\' || unicode.IsControl(r) }) {
			return ErrInvalidURLComponent
		}
		decoded, err := url.PathUnescape(component)
		if err != nil {
			if depth == 0 {
				return ErrInvalidURLComponent
			}
			decoded = escapedByte.ReplaceAllStringFunc(component, func(value string) string { v, _ := url.PathUnescape(value); return v })
		}
		if decoded == component {
			return nil
		}
		if depth == maxDepth {
			return ErrInvalidURLComponent
		}
		component = decoded
	}
}
