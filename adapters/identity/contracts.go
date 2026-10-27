// Package identity implements provider authentication and Identity Platform token exchange.
package identity

import (
	"context"
	"errors"
)

var (
	ErrNotImplemented = errors.New("not_implemented")
	ErrCredential     = errors.New("invalid_provider_credential")
	ErrUpstream       = errors.New("upstream_error")
	ErrTimeout        = errors.New("upstream_timeout")
	ErrRefresh        = errors.New("invalid_refresh_token")
)

type Tokens struct {
	Email        string `json:"-"`
	LocalID      string `json:"-"`
	IDToken      string `json:"idToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
}

// Credential carries the sign-in data required by an identity implementation.
type Credential[T any] struct{ Data T }

type PasswordCredential struct{ Email, Password string }

// GCIPCredential contains a GCIP-managed authorization response.
type GCIPCredential struct {
	ProviderID, RequestURI, SessionID string
}

// Authorization includes the GCIP state and server-side session.
// SessionID stays on the server and is bound to the browser transaction.
type Authorization struct{ URL, State, SessionID string }

type OAuthCallback struct{ Code, State, SessionID string }

// Provider starts GCIP-managed login and prepares its callback for sign-in.
type Provider interface {
	Start(context.Context) (Authorization, error)
	Exchange(context.Context, OAuthCallback) (Credential[GCIPCredential], error)
}

type Identity[T any] interface {
	SignIn(context.Context, Credential[T]) (Tokens, error)
	Refresh(context.Context, string) (Tokens, error)
}

// TokenVerifier validates an application ID token and returns its user ID.
type TokenVerifier interface {
	Verify(context.Context, string) (string, error)
}

var (
	_ TokenVerifier                = (*GCIPVerifier)(nil)
	_ Provider                     = (*GoogleGCIPProvider)(nil)
	_ Provider                     = (*GithubGCIPProvider)(nil)
	_ Identity[GCIPCredential]     = (*GCIP)(nil)
	_ Identity[PasswordCredential] = (*EmailPassword)(nil)
)
