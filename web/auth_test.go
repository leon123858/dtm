package web

import (
	"bytes"
	"context"
	"dtm/adapters/identity"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/config"
	"dtm/services/auth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct{}

func (fakeProvider) AuthorizationURL(state, verifier, nonce string) string {
	return "https://example.com?state=" + state
}
func (fakeProvider) Exchange(context.Context, string, string, string) (identity.Credential, error) {
	return identity.Credential{ProviderID: "google.com", IDToken: "provider-secret"}, nil
}

type fakeIdentity struct{}

func (fakeIdentity) SignIn(context.Context, identity.Credential) (identity.Tokens, error) {
	return identity.Tokens{LocalID: "member", IDToken: "jwt-secret", RefreshToken: "refresh-secret", ExpiresIn: 3600}, nil
}
func (fakeIdentity) Refresh(context.Context, string) (identity.Tokens, error) {
	return identity.Tokens{IDToken: "new-jwt", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
}
func testAuth(t *testing.T) (*gin.Engine, *auth.Service) {
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	t.Helper()
	gin.SetMode(gin.TestMode)
	s := &auth.Service{Store: mem.NewInMemoryAuthDBWrapper(), Providers: map[string]identity.Provider{"google": fakeProvider{}}, Identity: fakeIdentity{}}
	r := gin.New()
	setupMiddlewares(r, ServiceConfig{IsDev: true})
	setupAuthRoutes(r, s, config.Auth{FrontendURL: "http://localhost:3000", RedirectPath: "/backend/auth/callback"}, true)
	return r, s
}
func perform(r http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestAuthLoginAndExchange(t *testing.T) {
	t.Setenv("ADMIN_KEY", "admin-secret")
	r, _ := testAuth(t)
	start := perform(r, "GET", "/auth/google", "", nil)
	require.Equal(t, 302, start.Code)
	location, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	state := location.Query().Get("state")
	cookie := start.Result().Cookies()[0]
	require.True(t, cookie.HttpOnly)
	require.False(t, cookie.Secure)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	// A different browser cannot consume the transaction.
	wrong := perform(r, "GET", "/auth/callback?state="+state+"&code=provider-code", "", nil)
	require.Contains(t, wrong.Header().Get("Location"), "error=invalid_state")
	callback := perform(r, "GET", "/auth/callback?state="+state+"&code=provider-code", "", cookie)
	require.False(t, callback.Result().Cookies()[0].Secure)
	u, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	code := u.Query().Get("code")
	require.NotEmpty(t, code)
	require.Equal(t, "no-store", callback.Header().Get("Cache-Control"))
	exchange := perform(r, "POST", "/auth/exchange", `{"code":"`+code+`"}`, nil)
	require.Equal(t, 200, exchange.Code)
	var tokens auth.OAuthExchangeResult
	require.NoError(t, json.Unmarshal(exchange.Body.Bytes(), &tokens))
	require.Equal(t, "/", tokens.ReturnTo)
	require.Equal(t, "jwt-secret", tokens.IDToken)
	require.Equal(t, "refresh-secret", tokens.RefreshToken)
	require.Greater(t, tokens.ExpiresIn, int64(3590))
	require.Equal(t, 400, perform(r, "POST", "/auth/exchange", `{"code":"`+code+`"}`, nil).Code)
	require.Contains(t, perform(r, "GET", "/auth/callback?state="+state+"&code=replay", "", cookie).Header().Get("Location"), "error=invalid_state")
	require.Equal(t, 404, perform(r, "GET", "/auth/github", "", nil).Code)
	require.Equal(t, 200, perform(r, "POST", "/auth/refresh", `{"refreshToken":"refresh-secret"}`, nil).Code)
	for _, body := range []string{`{}`, `null`, `{"code":5}`, `{"code":"x"} {}`, `{"code":"x","codeVerifier":"y"}`} {
		require.Equal(t, 400, perform(r, "POST", "/auth/exchange", body, nil).Code)
	}
}
func TestAuthExpiryAndAtomicConsumption(t *testing.T) {
	_, s := testAuth(t)
	now := time.Now()
	s.Now = func() time.Time { return now }
	state, binding, _, err := s.OAuthStart(context.Background(), "google", "/")
	require.NoError(t, err)
	now = now.Add(10 * time.Minute)
	_, err = s.OAuthCallback(context.Background(), state, binding, "code", false)
	require.ErrorIs(t, err, auth.ErrState)
	state, binding, _, err = s.OAuthStart(context.Background(), "google", "/")
	require.NoError(t, err)
	code, err := s.OAuthCallback(context.Background(), state, binding, "code", false)
	require.NoError(t, err)
	now = now.Add(time.Minute)
	_, err = s.OAuthExchange(context.Background(), code)
	require.ErrorIs(t, err, db.ErrInvalidAuthEntry)
	state, binding, _, err = s.OAuthStart(context.Background(), "google", "/")
	require.NoError(t, err)
	code, err = s.OAuthCallback(context.Background(), state, binding, "code", false)
	require.NoError(t, err)
	var wg sync.WaitGroup
	var success atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.OAuthExchange(context.Background(), code); e == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), success.Load())
}
func TestAuthLogExclusionAndRecovery(t *testing.T) {
	var logs bytes.Buffer
	previousWriter, previousError, previousLogger := gin.DefaultWriter, gin.DefaultErrorWriter, logger
	gin.DefaultWriter = &logs
	gin.DefaultErrorWriter = &logs
	logger = slog.New(slog.NewJSONHandler(&logs, nil))
	t.Cleanup(func() {
		gin.DefaultWriter = previousWriter
		gin.DefaultErrorWriter = previousError
		logger = previousLogger
	})
	r, _ := testAuth(t)
	r.GET("/auth/panic/test", func(c *gin.Context) { panic("panic-token-secret") })
	r.POST("/ordinary", func(c *gin.Context) { c.Status(204) })
	perform(r, "POST", "/auth/refresh", `{"refreshToken":"private-refresh"}`, nil)
	perform(r, "GET", "/auth/callback?code=private-code&state=private-state", "", nil)
	require.Equal(t, 500, perform(r, "GET", "/auth/panic/test?code=panic-query-secret", "", nil).Code)
	start := perform(r, "GET", "/auth/google?returnTo=%2Fhistory", "", nil)
	location, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	cookie := start.Result().Cookies()[0]
	callback := perform(r, "GET", "/auth/callback?state="+location.Query().Get("state")+"&code=private-provider-code", "", cookie)
	completed, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	loginCode := completed.Query().Get("code")
	require.NotEmpty(t, loginCode)
	perform(r, "POST", "/auth/exchange", `{"code":"`+loginCode+`"}`, nil)
	text := logs.String()
	for _, secret := range []string{cookie.Value, loginCode, "private-provider-code", "jwt-secret", "refresh-secret", callback.Header().Get("Location")} {
		require.NotContains(t, text, secret)
	}
	for _, secret := range []string{"private-refresh", "private-code", "private-state", "panic-token-secret", "panic-query-secret", "/auth/"} {
		require.NotContains(t, text, secret)
	}
	logs.Reset()
	perform(r, "POST", "/ordinary", `{"value":"visible"}`, nil)
	require.Contains(t, logs.String(), "visible")
	require.Equal(t, 1, strings.Count(logs.String(), "/ordinary"))
}

func TestServeRejectsMissingAuthConfig(t *testing.T) {
	t.Setenv("GCIP_PROJECT_ID", "project")
	t.Chdir(t.TempDir())
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	for _, key := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GCIP_API_KEY"} {
		t.Setenv(key, "")
	}
	require.PanicsWithError(t, "GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET and GCIP_API_KEY must all be set", func() {
		Serve(ServiceConfig{IsDev: true, Port: "8080"})
	})
}

func TestAuthBindingCookieTransport(t *testing.T) {
	for _, tc := range []struct {
		name string
		dev  bool
		path string
	}{{"development", true, "/backend/auth/callback"}, {"production", false, "/backend/auth/callback"}, {"custom proxy", false, "/custom/auth/callback"}, {"same origin direct", false, "/auth/callback"}} {
		t.Run(tc.name, func(t *testing.T) {
			_, service := testAuth(t)
			r := gin.New()
			setupAuthRoutes(r, service, config.Auth{FrontendURL: "https://frontend.example", RedirectPath: tc.path}, tc.dev)
			start := perform(r, "GET", "/auth/google", "", nil)
			require.Equal(t, http.StatusFound, start.Code)
			cookies := start.Result().Cookies()
			require.Len(t, cookies, 1)
			require.Equal(t, !tc.dev, cookies[0].Secure)
			require.True(t, cookies[0].HttpOnly)
			require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
			require.Equal(t, tc.path, cookies[0].Path)
			require.Equal(t, config.OAuthTransactionSeconds, cookies[0].MaxAge)

			// Model browser transport rules without localhost exceptions.
			origin, err := url.Parse("https://192.168.1.10/backend/auth/google")
			require.NoError(t, err)
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			jar.SetCookies(origin, cookies)
			callbackURL, err := url.Parse("http://192.168.1.10" + tc.path)
			require.NoError(t, err)
			if tc.dev {
				require.Len(t, jar.Cookies(callbackURL), 1)
			} else {
				require.Empty(t, jar.Cookies(callbackURL))
				callbackURL.Scheme = "https"
			}
			location, err := url.Parse(start.Header().Get("Location"))
			require.NoError(t, err)
			callbackURL.RawQuery = url.Values{"state": {location.Query().Get("state")}, "code": {"provider-code"}}.Encode()
			available := jar.Cookies(callbackURL)
			require.Len(t, available, 1)
			// Forward the browser cookie after rewriting the public callback path.
			backendURL := *callbackURL
			backendURL.Path = "/auth/callback"
			if tc.path != backendURL.Path {
				require.Empty(t, jar.Cookies(&backendURL))
			}
			callback := perform(r, "GET", backendURL.String(), "", available[0])
			require.Contains(t, callback.Header().Get("Location"), "?code=")
			cleared := callback.Result().Cookies()
			require.Len(t, cleared, 1)
			require.Equal(t, !tc.dev, cleared[0].Secure)
			require.Equal(t, -1, cleared[0].MaxAge)
			require.True(t, cleared[0].HttpOnly)
			require.Equal(t, http.SameSiteLaxMode, cleared[0].SameSite)
			require.Equal(t, tc.path, cleared[0].Path)
			jar.SetCookies(callbackURL, cleared)
			require.Empty(t, jar.Cookies(callbackURL))
		})
	}
}

func TestAuthReturnToFlow(t *testing.T) {
	for _, destination := range []string{"", "/", "/history?search=a%20b#recent", "/trip/550e8400-e29b-41d4-a716-446655440000?tab=costs#record"} {
		t.Run(destination, func(t *testing.T) {
			r, _ := testAuth(t)
			start := perform(r, "GET", "/auth/google?"+url.Values{"returnTo": {destination}}.Encode(), "", nil)
			require.Equal(t, 302, start.Code)
			location, err := url.Parse(start.Header().Get("Location"))
			require.NoError(t, err)
			callback := perform(r, "GET", "/auth/callback?"+url.Values{"state": {location.Query().Get("state")}, "code": {"provider-code"}, "returnTo": {"//evil.example"}}.Encode(), "", start.Result().Cookies()[0])
			target, err := url.Parse(callback.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, "http://localhost:3000/auth/complete", target.Scheme+"://"+target.Host+target.Path)
			require.Len(t, target.Query(), 1)
			code := target.Query().Get("code")
			require.NotEmpty(t, code)
			bad := perform(r, "POST", "/auth/exchange", `{"code":"`+code+`","returnTo":"/history"}`, nil)
			require.Equal(t, 400, bad.Code)
			require.JSONEq(t, `{"error":"invalid_request"}`, bad.Body.String())
			exchange := perform(r, "POST", "/auth/exchange?returnTo=%2Fhistory", `{"code":"`+code+`"}`, nil)
			require.Equal(t, 200, exchange.Code)
			var result auth.OAuthExchangeResult
			require.NoError(t, json.Unmarshal(exchange.Body.Bytes(), &result))
			if destination == "" {
				destination = "/"
			}
			require.Equal(t, destination, result.ReturnTo)
			require.Equal(t, "jwt-secret", result.IDToken)
			refresh := perform(r, "POST", "/auth/refresh", `{"refreshToken":"test"}`, nil)
			require.NotContains(t, refresh.Body.String(), "returnTo")
		})
	}
}

func TestAuthReturnToTransactionsAreIndependent(t *testing.T) {
	r, _ := testAuth(t)
	type pending struct {
		state       string
		cookie      *http.Cookie
		destination string
	}
	var transactions []pending
	for _, destination := range []string{"/history", "/trip/550e8400-e29b-41d4-a716-446655440000"} {
		start := perform(r, "GET", "/auth/google?"+url.Values{"returnTo": {destination}}.Encode(), "", nil)
		location, err := url.Parse(start.Header().Get("Location"))
		require.NoError(t, err)
		transactions = append(transactions, pending{location.Query().Get("state"), start.Result().Cookies()[0], destination})
	}
	for i := len(transactions) - 1; i >= 0; i-- {
		tx := transactions[i]
		callback := perform(r, "GET", "/auth/callback?state="+tx.state+"&code=test", "", tx.cookie)
		location, err := url.Parse(callback.Header().Get("Location"))
		require.NoError(t, err)
		result := perform(r, "POST", "/auth/exchange", `{"code":"`+location.Query().Get("code")+`"}`, nil)
		var body auth.OAuthExchangeResult
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &body))
		require.Equal(t, tx.destination, body.ReturnTo)
	}
}

func TestAuthInvalidReturnTo(t *testing.T) {
	r, s := testAuth(t)
	// Any attempt to persist a transaction would panic and fail the response checks.
	s.Store = nil
	for _, query := range []string{"returnTo=%2F&returnTo=%2Fhistory", "returnTo=%2F&%72eturnTo=%2F", "returnTo=%zz", "returnTo=//evil.example", "returnTo=/auth/complete", "returnTo=/%252fhistory", "returnTo=/history%250a", "returnTo=/history;returnTo=/", "returnTo=" + url.QueryEscape("/history?q="+strings.Repeat("a", 4096)), "returnTo=" + url.QueryEscape("/history?q=%2525252541")} {
		response := perform(r, "GET", "/auth/google?"+query, "", nil)
		require.Equal(t, 400, response.Code, query)
		require.JSONEq(t, `{"error":"invalid_request"}`, response.Body.String())
		require.Empty(t, response.Result().Cookies())
		require.Empty(t, response.Header().Get("Location"))
	}
}

func TestLegacyTransactionReturnTo(t *testing.T) {
	r, s := testAuth(t)
	require.NoError(t, s.Store.PutLock(context.Background(), "transaction", auth.Hash("legacy-state"), auth.Hash("binding"), []byte(`{"Provider":"google","Verifier":"verifier","Nonce":"nonce"}`), time.Now().Add(time.Minute)))
	code, err := s.OAuthCallback(context.Background(), "legacy-state", "binding", "provider-code", false)
	require.NoError(t, err)
	response := perform(r, "POST", "/auth/exchange", `{"code":"`+code+`"}`, nil)
	var result auth.OAuthExchangeResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, "/", result.ReturnTo)
}
