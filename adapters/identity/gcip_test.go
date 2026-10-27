package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGCIPRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "api-key", r.URL.Query().Get("key"))
		if r.URL.Path == "/signin" {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "https://example.com/callback?code=a%2Bb%26c&state=state", body["requestUri"])
			require.Equal(t, "server-session", body["sessionId"])
			require.NotContains(t, body, "postBody")
			require.Equal(t, true, body["returnSecureToken"])
			w.Write([]byte(`{"email":"member@example.com","providerId":"google.com","localId":"member","idToken":"jwt","refreshToken":"refresh","expiresIn":"3600"}`))
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
	tokens, err := g.SignIn(context.Background(), Credential[GCIPCredential]{Data: GCIPCredential{ProviderID: "google.com", RequestURI: "https://example.com/callback?code=a%2Bb%26c&state=state", SessionID: "server-session"}})
	require.NoError(t, err)
	require.Equal(t, "jwt", tokens.IDToken)
	require.Equal(t, "member@example.com", tokens.Email)
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

func TestGCIPManagedSignInFailures(t *testing.T) {
	for _, provider := range managedProviders {
		t.Run(provider.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, body string
				status     int
				want       error
			}{
				{"upstream", `{"error":"secret"}`, 503, ErrUpstream},
				{"invalid session", `{"error":"secret"}`, 400, ErrUpstream},
				{"malformed", `secret`, 200, ErrUpstream},
				{"confirmation", `{"needConfirmation":true}`, 200, ErrCredential},
				{"wrong provider", `{"providerId":"other.com","localId":"member","idToken":"id","refreshToken":"refresh","expiresIn":"3600"}`, 200, ErrCredential},
				{"missing provider", `{"localId":"member","idToken":"id","refreshToken":"refresh","expiresIn":"3600"}`, 200, ErrCredential},
			} {
				t.Run(tc.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
					defer server.Close()
					gcip := NewGCIP(server.Client(), "key", "https://frontend.example/callback")
					gcip.SignInURL = server.URL
					result, err := gcip.SignIn(context.Background(), Credential[GCIPCredential]{Data: GCIPCredential{ProviderID: provider.providerID, RequestURI: "https://frontend.example/callback?state=x&code=y", SessionID: "session"}})
					require.ErrorIs(t, err, tc.want)
					require.Zero(t, result)
					require.NotContains(t, err.Error(), "secret")
				})
			}
			for _, field := range []string{"localId", "idToken", "refreshToken", "expiresIn"} {
				t.Run("missing "+field, func(t *testing.T) {
					body := map[string]string{"providerId": provider.providerID, "localId": "member", "idToken": "id", "refreshToken": "refresh", "expiresIn": "3600"}
					delete(body, field)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(body) }))
					defer server.Close()
					gcip := NewGCIP(server.Client(), "key", "")
					gcip.SignInURL = server.URL
					result, err := gcip.SignIn(context.Background(), Credential[GCIPCredential]{Data: GCIPCredential{ProviderID: provider.providerID, RequestURI: "https://frontend.example/callback?state=x&code=y", SessionID: "session"}})
					require.ErrorIs(t, err, ErrUpstream)
					require.Zero(t, result)
				})
			}
		})
	}
	gcip := NewGCIP(&http.Client{Transport: verifierRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid credentials must not reach upstream")
		return nil, nil
	})}, "key", "")
	for _, credential := range []GCIPCredential{{}, {ProviderID: "google.com", RequestURI: "https://example.com/callback"}, {ProviderID: "google.com", SessionID: "session"}, {RequestURI: "https://example.com/callback", SessionID: "session"}} {
		result, err := gcip.SignIn(context.Background(), Credential[GCIPCredential]{Data: credential})
		require.ErrorIs(t, err, ErrCredential)
		require.Zero(t, result)
	}
}
