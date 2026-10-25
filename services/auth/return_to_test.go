package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dtm/adapters/db/mem"
	"dtm/adapters/identity"
	"github.com/stretchr/testify/require"
)

func TestValidateReturnTo(t *testing.T) {
	for _, input := range []string{"", "/", "/history", "/trip/550e8400-e29b-41d4-a716-446655440000?tab=costs#record", "/history?q=a%20b&discount=100%25#中文", "/?next=https%3A%2F%2Fexample.com"} {
		got, err := ValidateReturnTo(input)
		require.NoError(t, err, input)
		if input == "" {
			input = "/"
		}
		require.Equal(t, input, got)
	}
	for _, input := range []string{
		"https://example.com", "//example.com", "///history", "history", " ", "/auth/complete", "/backend/auth/google", "/trip/abc123", "/history/", "/history/../", "/./history", "/trip//", "/%68istory", "/%2fhistory", "/%252fhistory", "/history%3fignored", "/history\\", "/history\n", "/history?q=%0a", "/history?q=%255c", "/history?q=%25&x=%255c", "/history#%250d", "/history?bad=%zz", "/history#%zz", "/history?q=%7f",
	} {
		_, err := ValidateReturnTo(input)
		require.ErrorIs(t, err, ErrRequest, input)
	}
}

func TestInvalidReturnToDoesNotStartTransaction(t *testing.T) {
	// Nil dependencies would panic if validation touched storage or a provider.
	s := &Service{}
	state, binding, location, err := s.OAuthStart(context.Background(), "google", "//evil.example")
	require.ErrorIs(t, err, ErrRequest)
	require.Empty(t, state)
	require.Empty(t, binding)
	require.Empty(t, location)
}

func TestLegacyLoginAndRemainingLifetime(t *testing.T) {
	now := time.Now()
	s := &Service{Store: mem.NewInMemoryAuthDBWrapper(), Now: func() time.Time { return now }}
	data, err := json.Marshal(struct {
		Tokens    identity.Tokens
		ExpiresAt time.Time
	}{identity.Tokens{IDToken: "test", ExpiresIn: 3600}, now.Add(3600 * time.Second)})
	require.NoError(t, err)
	require.NoError(t, s.Store.PutLock(context.Background(), "login", Hash("legacy"), "", data, now.Add(time.Minute)))
	now = now.Add(time.Second)
	result, err := s.OAuthExchange(context.Background(), "legacy")
	require.NoError(t, err)
	require.Equal(t, "/", result.ReturnTo)
	require.Equal(t, int64(3599), result.ExpiresIn)
}

func TestReturnToResourceLimits(t *testing.T) {
	const prefix = "/history?q="
	atLimit := prefix + strings.Repeat("a", maxReturnToBytes-len(prefix))
	got, err := ValidateReturnTo(atLimit)
	require.NoError(t, err)
	require.Equal(t, atLimit, got)
	nested := func(depth int) string { return prefix + "%" + strings.Repeat("25", depth-1) + "41" }
	got, err = ValidateReturnTo(nested(maxReturnToDecodeDepth))
	require.NoError(t, err)
	require.Equal(t, nested(maxReturnToDecodeDepth), got)
	for _, input := range []string{
		atLimit + "a",
		nested(maxReturnToDecodeDepth + 1),
		strings.Replace(nested(maxReturnToDecodeDepth+1), "?q=", "#", 1),
		nested(30000), // Reviewer regression: ~60 KB quadratic decoding input.
	} {
		// Rejection must happen before any storage or provider access.
		s := &Service{}
		state, binding, location, err := s.OAuthStart(context.Background(), "google", input)
		require.ErrorIs(t, err, ErrRequest)
		require.Empty(t, state)
		require.Empty(t, binding)
		require.Empty(t, location)
	}
}
