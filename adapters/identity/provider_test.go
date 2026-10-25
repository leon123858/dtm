package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var managedProviders = []struct {
	name, providerID, authURL string
	newProvider               func(*GCIP) Provider
}{
	{"google", "google.com", "https://accounts.google.com/o/oauth2/v2/auth", func(g *GCIP) Provider { return NewGoogle(g) }},
	{"google legacy", "google.com", "https://accounts.google.com/o/oauth2/auth", func(g *GCIP) Provider { return NewGoogle(g) }},
	{"github", "github.com", "https://github.com/login/oauth/authorize", func(g *GCIP) Provider { return NewGitHub(g) }},
}

func TestGCIPProviderFlow(t *testing.T) {
	for _, tc := range managedProviders {
		t.Run(tc.name, func(t *testing.T) {
			callback := "https://frontend.example/backend/auth/callback"
			state := "gcip+state&value"
			location := tc.authURL + "?state=" + url.QueryEscape(state)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "api-key", r.URL.Query().Get("key"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if r.URL.Path == "/start" {
					expected := map[string]any{"providerId": tc.providerID, "continueUri": callback, "oauthScope": "user:email"}
					if tc.providerID == "google.com" {
						expected["oauthScope"] = "openid email profile"
						expected["authFlowType"] = "CODE_FLOW"
						expected["customParameter"] = map[string]any{"prompt": "select_account"}
					}
					require.Equal(t, expected, body)
					json.NewEncoder(w).Encode(map[string]string{"authUri": location, "sessionId": "server-session"})
					return
				}
				require.Equal(t, "/signin", r.URL.Path)
				uri, err := url.Parse(body["requestUri"].(string))
				require.NoError(t, err)
				require.Equal(t, callback, uri.Scheme+"://"+uri.Host+uri.Path)
				require.Equal(t, url.Values{"state": {state}, "code": {"provider+code&value"}}, uri.Query())
				require.Equal(t, map[string]any{"requestUri": uri.String(), "sessionId": "server-session", "returnSecureToken": true}, body)
				json.NewEncoder(w).Encode(map[string]string{"providerId": tc.providerID, "localId": "member", "idToken": "gcip-id", "refreshToken": "gcip-refresh", "expiresIn": "3600"})
			}))
			defer server.Close()
			gcip := NewGCIP(server.Client(), "api-key", callback)
			gcip.AuthURIURL, gcip.SignInURL = server.URL+"/start", server.URL+"/signin"
			provider := tc.newProvider(gcip)
			authorization, err := provider.Start(context.Background())
			require.NoError(t, err)
			require.Equal(t, Authorization{URL: location, State: state, SessionID: "server-session"}, authorization)
			credential, err := provider.Exchange(context.Background(), OAuthCallback{Code: "provider+code&value", State: state, SessionID: authorization.SessionID})
			require.NoError(t, err)
			tokens, err := gcip.SignIn(context.Background(), credential)
			require.NoError(t, err)
			require.Equal(t, Tokens{LocalID: "member", IDToken: "gcip-id", RefreshToken: "gcip-refresh", ExpiresIn: 3600}, tokens)
			require.Equal(t, 2, calls)
		})
	}
}

func TestGCIPProviderRejectsInvalidAuthorization(t *testing.T) {
	for _, tc := range managedProviders {
		t.Run(tc.name, func(t *testing.T) {
			for _, invalid := range []struct{ name, uri, session string }{
				{"missing session", tc.authURL + "?state=x", ""},
				{"missing state", tc.authURL, "session"},
				{"duplicate state", tc.authURL + "?state=x&state=y", "session"},
				{"invalid query", tc.authURL + "?state=%zz", "session"},
				{"long state", tc.authURL + "?state=" + strings.Repeat("x", 4097), "session"},
				{"other provider", map[string]string{"google.com": "https://github.com/login/oauth/authorize?state=x", "github.com": "https://accounts.google.com/o/oauth2/auth?state=x"}[tc.providerID], "session"},
				{"wrong host", "https://evil.example/login?state=x", "session"},
				{"http", strings.Replace(tc.authURL, "https:", "http:", 1) + "?state=x", "session"},
				{"userinfo", strings.Replace(tc.authURL, "https://", "https://user@", 1) + "?state=x", "session"},
				{"fragment", tc.authURL + "?state=x#fragment", "session"},
				{"wrong path", tc.authURL + "/other?state=x", "session"},
			} {
				t.Run(invalid.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						json.NewEncoder(w).Encode(map[string]string{"authUri": invalid.uri, "sessionId": invalid.session})
					}))
					defer server.Close()
					gcip := NewGCIP(server.Client(), "key", "https://frontend.example/callback")
					gcip.AuthURIURL = server.URL
					result, err := tc.newProvider(gcip).Start(context.Background())
					require.ErrorIs(t, err, ErrUpstream)
					require.Zero(t, result)
				})
			}
		})
	}
}

func TestGCIPProviderFailures(t *testing.T) {
	for _, tc := range managedProviders {
		t.Run(tc.name, func(t *testing.T) {
			for _, failure := range []struct {
				name, body string
				status     int
			}{
				{"upstream", `{"error":"secret"}`, 503},
				{"malformed", `secret`, 200},
				{"empty", `{}`, 200},
			} {
				t.Run(failure.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(failure.status)
						w.Write([]byte(failure.body))
					}))
					defer server.Close()
					gcip := NewGCIP(server.Client(), "key", "https://frontend.example/callback")
					gcip.AuthURIURL = server.URL
					result, err := tc.newProvider(gcip).Start(context.Background())
					require.ErrorIs(t, err, ErrUpstream)
					require.Zero(t, result)
					require.NotContains(t, err.Error(), "secret")
				})
			}
			gcip := NewGCIP(&http.Client{Transport: verifierRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}, "key", "https://frontend.example/callback")
			_, err := tc.newProvider(gcip).Start(context.Background())
			require.ErrorIs(t, err, ErrTimeout)
			for _, callback := range []OAuthCallback{{}, {Code: "code", State: "state"}, {Code: "code", SessionID: "session"}, {State: "state", SessionID: "session"}} {
				result, err := tc.newProvider(gcip).Exchange(context.Background(), callback)
				require.ErrorIs(t, err, ErrCredential)
				require.Zero(t, result)
			}
		})
	}
}
