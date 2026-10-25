package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"dtm/adapters/db/mem"
	"dtm/adapters/identity"
	"dtm/config"
	"dtm/services/auth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGCIPManagedLogin(t *testing.T) {
	for _, tc := range []struct {
		name, providerID, authURL string
		newProvider               func(*identity.GCIP) identity.Provider
	}{
		{"google", "google.com", "https://accounts.google.com/o/oauth2/v2/auth", func(g *identity.GCIP) identity.Provider { return identity.NewGoogle(g) }},
		{"github", "github.com", "https://github.com/login/oauth/authorize", func(g *identity.GCIP) identity.Provider { return identity.NewGitHub(g) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Auth{FrontendURL: "https://frontend.example", RedirectPath: "/backend/auth/callback", RedirectURL: "https://frontend.example/backend/auth/callback"}
			sessions := map[string]string{}
			starts, signins := 0, 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if r.URL.Path == "/start" {
					require.Equal(t, tc.providerID, body["providerId"])
					starts++
					state := fmt.Sprintf("gcip-state-%d-", starts) + strings.Repeat("x", 160)
					session := fmt.Sprintf("private-session-%d", starts)
					sessions[state] = session
					require.Equal(t, cfg.RedirectURL, body["continueUri"])
					json.NewEncoder(w).Encode(map[string]string{"authUri": tc.authURL + "?state=" + state, "sessionId": session})
					return
				}
				signins++
				callback, err := url.Parse(body["requestUri"].(string))
				require.NoError(t, err)
				require.Equal(t, cfg.RedirectURL, callback.Scheme+"://"+callback.Host+callback.Path)
				state := callback.Query().Get("state")
				require.NotEmpty(t, sessions[state])
				require.Equal(t, sessions[state], body["sessionId"])
				require.Equal(t, "code-"+state, callback.Query().Get("code"))
				json.NewEncoder(w).Encode(map[string]string{"providerId": tc.providerID, "localId": "member", "idToken": "gcip-id", "refreshToken": "gcip-refresh", "expiresIn": "3600"})
			}))
			defer upstream.Close()
			gcip := identity.NewGCIP(upstream.Client(), "key", cfg.RedirectURL)
			gcip.AuthURIURL, gcip.SignInURL = upstream.URL+"/start", upstream.URL+"/signin"
			store := mem.NewInMemoryAuthDBWrapper()
			service, err := auth.NewServiceWithDependencies(context.Background(), cfg, auth.Dependencies{Store: store, Providers: map[string]identity.Provider{tc.name: tc.newProvider(gcip)}, Identity: gcip})
			require.NoError(t, err)
			t.Cleanup(service.Close)
			router := gin.New()
			setupAuthRoutes(router, service, cfg, false)
			type pending struct {
				state, destination string
				cookie             *http.Cookie
			}
			var logins []pending
			for _, destination := range []string{"/history", "/"} {
				response := perform(router, "GET", "/auth/"+tc.name+"?returnTo="+url.QueryEscape(destination), "", nil)
				require.Equal(t, 302, response.Code)
				location, err := url.Parse(response.Header().Get("Location"))
				require.NoError(t, err)
				require.NotContains(t, response.Header().Get("Location"), "private-session")
				cookie := response.Result().Cookies()[0]
				require.NotContains(t, cookie.Value, "private-session")
				logins = append(logins, pending{location.Query().Get("state"), destination, cookie})
			}
			for i := len(logins) - 1; i >= 0; i-- {
				login := logins[i]
				path := "/auth/callback?" + url.Values{"state": {login.state}, "code": {"code-" + login.state}, "sessionId": {"attacker-session"}}.Encode()
				wrong := perform(router, "GET", path, "", logins[1-i].cookie)
				require.Contains(t, wrong.Header().Get("Location"), "error=invalid_state")
				response := perform(router, "GET", path, "", login.cookie)
				location, err := url.Parse(response.Header().Get("Location"))
				require.NoError(t, err)
				code := location.Query().Get("code")
				require.NotEmpty(t, code)
				exchange := perform(router, "POST", "/auth/exchange", `{"code":"`+code+`"}`, nil)
				require.Equal(t, 200, exchange.Code)
				var tokens auth.OAuthExchangeResult
				require.NoError(t, json.Unmarshal(exchange.Body.Bytes(), &tokens))
				require.Equal(t, login.destination, tokens.ReturnTo)
				require.Equal(t, "gcip-id", tokens.IDToken)
				require.Equal(t, 400, perform(router, "POST", "/auth/exchange", `{"code":"`+code+`"}`, nil).Code)
				require.Contains(t, perform(router, "GET", path, "", login.cookie).Header().Get("Location"), "error=invalid_state")
			}
			require.Equal(t, 2, signins)
			_, err = store.UserGCID2UserId(context.Background(), "member")
			require.NoError(t, err)
			response := perform(router, "GET", "/auth/"+tc.name, "", nil)
			location, err := url.Parse(response.Header().Get("Location"))
			require.NoError(t, err)
			denied := perform(router, "GET", "/auth/callback?state="+location.Query().Get("state")+"&error=access_denied", "", response.Result().Cookies()[0])
			require.Contains(t, denied.Header().Get("Location"), "error=access_denied")
			require.Equal(t, 2, signins)
		})
	}
}
