// Package identity implements provider authentication and Identity Platform token exchange.
package identity

import (
	"context"
	"errors"
)

var (
	ErrCredential = errors.New("invalid_provider_credential")
	ErrUpstream   = errors.New("upstream_error")
	ErrTimeout    = errors.New("upstream_timeout")
	ErrRefresh    = errors.New("invalid_refresh_token")
)

type Tokens struct {
	LocalID      string `json:"-"`
	IDToken      string `json:"idToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
}

type Credential struct{ ProviderID, IDToken, AccessToken string }

type Provider interface {
	AuthorizationURL(state, verifier, nonce string) string
	Exchange(context.Context, string, string, string) (Credential, error)
}

type Identity interface {
	SignIn(context.Context, Credential) (Tokens, error)
	Refresh(context.Context, string) (Tokens, error)
}

// PasswordProvider signs in directly without an OAuth redirect or code exchange.
type PasswordProvider interface {
	SignIn(context.Context, string, string) (Tokens, error)
}
