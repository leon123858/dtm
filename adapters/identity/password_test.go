package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEmailPassword(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"success", 200, `{"localId":"member","idToken":"jwt","refreshToken":"refresh","expiresIn":"3600"}`, nil},
		{"invalid login", 400, `{"error":{"message":"INVALID_LOGIN_CREDENTIALS"}}`, ErrCredential},
		{"password", 400, `{"error":{"message":"INVALID_PASSWORD"}}`, ErrCredential},
		{"missing account", 400, `{"error":{"message":"EMAIL_NOT_FOUND"}}`, ErrCredential},
		{"disabled", 400, `{"error":{"message":"USER_DISABLED"}}`, ErrCredential},
		{"configuration", 400, `{"error":{"message":"API_KEY_INVALID secret"}}`, ErrUpstream},
		{"upstream", 500, `secret`, ErrUpstream},
		{"redirect", 302, `secret`, ErrUpstream},
		{"malformed", 200, `{`, ErrUpstream},
		{"missing local id", 200, `{"idToken":"jwt","refreshToken":"refresh","expiresIn":"3600"}`, ErrUpstream},
		{"missing id token", 200, `{"localId":"member","refreshToken":"refresh","expiresIn":"3600"}`, ErrUpstream},
		{"missing refresh", 200, `{"localId":"member","idToken":"jwt","expiresIn":"3600"}`, ErrUpstream},
		{"invalid expiry", 200, `{"localId":"member","idToken":"jwt","refreshToken":"refresh","expiresIn":"0"}`, ErrUpstream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "key+&", r.URL.Query().Get("key"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, map[string]any{"email": "test@example.com", "password": " secret+& ", "returnSecureToken": true}, body)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			p := NewEmailPassword(server.Client(), "key+&")
			p.SignInURL = server.URL
			tokens, err := p.SignIn(context.Background(), "test@example.com", " secret+& ")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, tokens)
				require.NotContains(t, err.Error(), "secret")
			} else {
				require.NoError(t, err)
				require.Equal(t, Tokens{LocalID: "member", IDToken: "jwt", RefreshToken: "refresh", ExpiresIn: 3600}, tokens)
			}
		})
	}
}

func TestEmailPasswordTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	p := NewEmailPassword(http.DefaultClient, "key")
	_, err := p.SignIn(ctx, "email", "password")
	require.ErrorIs(t, err, ErrTimeout)
}
