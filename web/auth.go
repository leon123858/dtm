package web

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/adapters/identity"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"dtm/config"
	"dtm/services/auth"
	"github.com/gin-gonic/gin"
)

func isAuthPath(path string) bool { return path == "/auth" || strings.HasPrefix(path, "/auth/") }

// SECURITY: Cloud Run, reverse proxies and frontend hosting may log full URLs
// even though application access/body logging skips /auth. URLs can contain
// intermediate login credentials (authorization code, state, one-time Login Code).
// Protect production log access, retention and sensitive information accordingly.
func setupAuthRoutes(r *gin.Engine, s auth.Service, cfg config.Auth, dev bool) {
	group := r.Group("/auth", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	cookieName := func(state string) string { return "oauth_" + auth.Hash(state) }
	group.POST("/password", func(c *gin.Context) {
		if !cfg.E2EAuthEnabled {
			authFailure(c, "password", auth.ErrUnauthenticated)
			return
		}
		tokens, err := s.PasswordLogin(c.Request.Context())
		if errors.Is(err, identity.ErrCredential) {
			err = auth.ErrUnauthenticated
		}
		if err != nil {
			authFailure(c, "password", err)
			return
		}
		c.JSON(http.StatusOK, tokens)
	})
	setCookie := func(c *gin.Context, state, value string, age int) {
		// The frontend proxies /backend/auth/* to /auth/*. Scope the cookie to the
		// browser-facing callback path so it is sent before Next.js rewrites the request.
		http.SetCookie(c.Writer, &http.Cookie{Name: cookieName(state), Value: value, Path: cfg.RedirectPath, MaxAge: age, HttpOnly: true, Secure: !dev, SameSite: http.SameSiteLaxMode})
	}
	group.GET("/callback", func(c *gin.Context) {
		state := c.Query("state")
		binding, _ := c.Cookie(cookieName(state))
		code, err := s.OAuthCallback(c.Request.Context(), state, binding, c.Query("code"), c.Query("error") != "")
		setCookie(c, state, "", -1)
		q := url.Values{}
		if err != nil {
			q.Set("error", authErrorCode(err))
			logAuthError("callback", err)
		} else {
			q.Set("code", code)
		}
		c.Redirect(http.StatusFound, cfg.FrontendURL+"/auth/complete?"+q.Encode())
	})
	group.GET("/:provider", func(c *gin.Context) {
		query, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil || len(query["returnTo"]) > 1 {
			authFailure(c, "start", auth.ErrRequest)
			return
		}
		state, binding, location, err := s.OAuthStart(c.Request.Context(), c.Param("provider"), query.Get("returnTo"))
		if err != nil {
			authFailure(c, "start", err)
			return
		}
		setCookie(c, state, binding, config.OAuthTransactionSeconds)
		c.Redirect(http.StatusFound, location)
	})
	group.POST("/exchange", func(c *gin.Context) {
		var body struct {
			Code string `json:"code"`
		}
		if !authJSON(c, &body) {
			return
		}
		result, err := s.OAuthExchange(c.Request.Context(), body.Code)
		if err != nil {
			authFailure(c, "exchange", err)
			return
		}
		c.JSON(200, result)
	})
	group.POST("/refresh", func(c *gin.Context) {
		var body struct {
			RefreshToken string `json:"refreshToken"`
		}
		if !authJSON(c, &body) {
			return
		}
		if body.RefreshToken == "" {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		result, err := s.Refresh(c.Request.Context(), body.RefreshToken)
		if err != nil {
			authFailure(c, "refresh", err)
			return
		}
		c.JSON(200, result)
	})
}
func authJSON(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		c.JSON(400, gin.H{"error": "invalid_request"})
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		c.JSON(400, gin.H{"error": "invalid_request"})
		return false
	}
	return true
}
func authErrorCode(err error) string {
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.ErrUnauthenticated.Error()
	}
	for _, safe := range []error{auth.ErrRequest, db.ErrInvalidAuthEntry, auth.ErrState, auth.ErrProvider, identity.ErrCredential, identity.ErrUpstream, identity.ErrTimeout, identity.ErrRefresh, auth.ErrStorage, auth.ErrDenied} {
		if errors.Is(err, safe) {
			return safe.Error()
		}
	}
	return "internal_error"
}
func logAuthError(stage string, err error) {
	logger.Warn("auth failed", "stage", stage, "code", authErrorCode(err))
}
func authFailure(c *gin.Context, stage string, err error) {
	status := 500
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		status = http.StatusUnauthorized
	case errors.Is(err, auth.ErrRequest), errors.Is(err, db.ErrInvalidAuthEntry), errors.Is(err, auth.ErrState), errors.Is(err, identity.ErrCredential), errors.Is(err, auth.ErrDenied):
		status = 400
	case errors.Is(err, auth.ErrProvider):
		status = 404
	case errors.Is(err, identity.ErrRefresh):
		status = 401
	case errors.Is(err, identity.ErrUpstream):
		status = 502
	case errors.Is(err, identity.ErrTimeout):
		status = 504
	}
	logAuthError(stage, err)
	c.JSON(status, gin.H{"error": authErrorCode(err)})
}
