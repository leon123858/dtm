package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGCIPRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "api-key", r.URL.Query().Get("key"))
		if r.URL.Path == "/signin" {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			form, err := url.ParseQuery(body["postBody"].(string))
			require.NoError(t, err)
			require.Equal(t, "google.com", form.Get("providerId"))
			require.Equal(t, "a+b&c", form.Get("id_token"))
			require.Equal(t, true, body["returnSecureToken"])
			w.Write([]byte(`{"localId":"member","idToken":"jwt","refreshToken":"refresh","expiresIn":"3600"}`))
		} else {
			require.NoError(t, r.ParseForm())
			require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
			require.Equal(t, "old+token", r.Form.Get("refresh_token"))
			w.Write([]byte(`{"id_token":"new-jwt","refresh_token":"rotated","expires_in":"3600"}`))
		}
	}))
	defer server.Close()
	g := NewGCIP(server.Client(), "api-key", "http://localhost:8080/auth/callback")
	g.SignInURL = server.URL + "/signin"
	g.RefreshURL = server.URL + "/refresh"
	tokens, err := g.SignIn(context.Background(), Credential{ProviderID: "google.com", IDToken: "a+b&c"})
	require.NoError(t, err)
	require.Equal(t, "jwt", tokens.IDToken)
	tokens, err = g.Refresh(context.Background(), "old+token")
	require.NoError(t, err)
	require.Equal(t, "rotated", tokens.RefreshToken)
}
func TestGCIPDoesNotExposeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"sensitive-token"}`))
	}))
	defer server.Close()
	g := NewGCIP(server.Client(), "secret", "")
	g.RefreshURL = server.URL
	_, err := g.Refresh(context.Background(), "refresh")
	require.ErrorIs(t, err, ErrRefresh)
	require.NotContains(t, err.Error(), "sensitive")
}

func TestGCIPSignInRequiresLocalID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"idToken":"jwt","refreshToken":"refresh","expiresIn":"3600"}`))
	}))
	defer server.Close()
	g := NewGCIP(server.Client(), "key", "https://example.com/callback")
	g.SignInURL = server.URL
	_, err := g.SignIn(context.Background(), Credential{ProviderID: "google.com", IDToken: "provider"})
	require.ErrorIs(t, err, ErrUpstream)
}
