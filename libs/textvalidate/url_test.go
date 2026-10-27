package textvalidate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidOrigin(t *testing.T) {
	for _, raw := range []string{"https://example.com", "https://example.com:443"} {
		require.True(t, ValidOrigin(raw, false), raw)
	}
	require.True(t, ValidOrigin("http://localhost:8080", true))
	for _, raw := range []string{"", "http://example.com", "//example.com", "https://user@example.com", "https://example.com/path", "https://example.com?q=1", "https://example.com#fragment"} {
		require.False(t, ValidOrigin(raw, false), raw)
	}
}

func TestValidRedirectPath(t *testing.T) {
	require.True(t, ValidRedirectPath("/auth/google/callback"))
	for _, raw := range []string{"", "/", "//evil.example", "https://evil.example/auth", "/auth/../callback", "/auth/", "/auth%2fcallback", "/auth?", "/auth?q=1", "/auth#fragment", "/auth\\callback", "/auth\n"} {
		require.False(t, ValidRedirectPath(raw), raw)
	}
}

func TestValidateEncodedURLComponent(t *testing.T) {
	for _, raw := range []string{"", "中文", "q=a%20b", "discount=100%25", "%25252541"} {
		require.NoError(t, ValidateEncodedURLComponent(raw, 4), raw)
	}
	for _, raw := range []string{"\xff", "\n", "\\", "%0a", "%255c", "%25&x=%255c", "%zz", "%25ff", "%2525252541"} {
		require.ErrorIs(t, ValidateEncodedURLComponent(raw, 4), ErrInvalidURLComponent, raw)
	}
	require.NoError(t, ValidateEncodedURLComponent("literal", 0))
	require.ErrorIs(t, ValidateEncodedURLComponent("%41", 0), ErrInvalidURLComponent)
	require.ErrorIs(t, ValidateEncodedURLComponent("literal", -1), ErrInvalidURLComponent)
	// Names permit markup and backslashes; URL components have a different policy.
	require.NoError(t, ValidateName("<name>\\"))
	require.NoError(t, ValidateEncodedURLComponent(strings.Repeat("a", 4096), 4))
}

func TestParseUnescapedRelativeURL(t *testing.T) {
	for _, raw := range []string{"/", "/history?q=a%20b#record", "/trip/123"} {
		parsed, err := ParseUnescapedRelativeURL(raw)
		require.NoError(t, err)
		require.Equal(t, raw, parsed.String())
	}
	for _, raw := range []string{"https://evil.example", "//evil.example", "///history", "/%68istory", "/history%2fsecret", "/%252fhistory", "/history%3fignored"} {
		_, err := ParseUnescapedRelativeURL(raw)
		require.ErrorIs(t, err, ErrInvalidURLComponent, raw)
	}
}
