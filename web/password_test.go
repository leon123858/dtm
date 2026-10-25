package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"dtm/adapters/db/mem"
	"dtm/adapters/identity"
	"dtm/config"
	"dtm/services/auth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type webPasswordProvider struct {
	calls int
	err   error
}

func (p *webPasswordProvider) SignIn(context.Context, string, string) (identity.Tokens, error) {
	p.calls++
	return identity.Tokens{LocalID: "member", IDToken: "password-jwt-secret", RefreshToken: "password-refresh-secret", ExpiresIn: 3600}, p.err
}

func TestPasswordRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		enabled bool
		err     error
		status  int
	}{
		{"disabled", false, nil, 401}, {"enabled", true, nil, 200},
		{"credentials", true, identity.ErrCredential, 401},
		{"upstream", true, identity.ErrUpstream, 502}, {"timeout", true, identity.ErrTimeout, 504},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			old := logger
			logger = slog.New(slog.NewTextHandler(&logs, nil))
			t.Cleanup(func() { logger = old })
			provider := &webPasswordProvider{err: tc.err}
			s := &auth.Service{Store: mem.NewInMemoryAuthDBWrapper(), PasswordProvider: provider}
			r := gin.New()
			setupMiddlewares(r, ServiceConfig{IsDev: true})
			setupAuthRoutes(r, s, config.Auth{E2EAuthEnabled: tc.enabled}, true)
			// Caller credentials are never used or logged, even when supplied.
			w := perform(r, "POST", "/auth/password", `{"password":"caller-password-secret"}`, nil)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if !tc.enabled {
				require.Zero(t, provider.calls)
			} else {
				require.Equal(t, 1, provider.calls)
			}
			if tc.status == 200 {
				var body map[string]any
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				require.Equal(t, map[string]any{"idToken": "password-jwt-secret", "refreshToken": "password-refresh-secret", "expiresIn": float64(3600)}, body)
			} else {
				require.NotContains(t, w.Body.String(), "secret")
				if tc.status == 401 {
					require.JSONEq(t, `{"error":"UNAUTHENTICATED"}`, w.Body.String())
				}
			}
			for _, secret := range []string{"caller-password-secret", "password-jwt-secret", "password-refresh-secret"} {
				require.NotContains(t, logs.String(), secret)
			}
		})
	}
}
