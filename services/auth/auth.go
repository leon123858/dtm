// Package auth coordinates OAuth login, member identity, and trip subscriptions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"dtm/adapters/db/db"
	"dtm/adapters/identity"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"dtm/config"
)

var (
	ErrRequest         = errors.New("invalid_request")
	ErrState           = errors.New("invalid_state")
	ErrProvider        = errors.New("unsupported_provider")
	ErrStorage         = errors.New("storage_error")
	ErrDenied          = errors.New("access_denied")
	ErrUnauthenticated = errors.New("UNAUTHENTICATED")
)

type transaction struct{ Provider, Verifier, Nonce, ReturnTo string }
type login struct {
	Tokens    identity.Tokens
	ExpiresAt time.Time
	ReturnTo  string
}

// OAuthExchangeResult includes the destination bound to the consumed Login Code.
type OAuthExchangeResult struct {
	identity.Tokens
	ReturnTo string `json:"returnTo"`
}

type Service struct {
	cancel                        context.CancelFunc
	done                          chan struct{}
	Store                         db.AuthDBWrapper
	Providers                     map[string]identity.Provider
	Identity                      identity.Identity
	PasswordProvider              identity.PasswordProvider
	passwordEmail, passwordSecret string
	Now                           func() time.Time
}

// PasswordLogin uses only the configured E2E account. A nil provider disables it.
func (s *Service) PasswordLogin(ctx context.Context) (identity.Tokens, error) {
	if s.PasswordProvider == nil {
		return identity.Tokens{}, ErrUnauthenticated
	}
	tokens, err := s.PasswordProvider.SignIn(ctx, s.passwordEmail, s.passwordSecret)
	if err != nil {
		return identity.Tokens{}, err
	}
	if tokens.LocalID == "" {
		return identity.Tokens{}, identity.ErrUpstream
	}
	if _, err := s.Store.UpsertUser(ctx, tokens.LocalID); err != nil {
		return identity.Tokens{}, ErrStorage
	}
	return tokens, nil
}

func Random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Hash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) OAuthStart(ctx context.Context, provider, returnTo string) (state, binding, location string, err error) {
	returnTo, err = ValidateReturnTo(returnTo)
	if err != nil {
		return
	}
	p, ok := s.Providers[provider]
	if !ok {
		err = ErrProvider
		return
	}
	state, binding = Random(), Random()
	tx := transaction{Provider: provider, Verifier: Random(), Nonce: Random(), ReturnTo: returnTo}
	data, _ := json.Marshal(tx)
	if e := s.Store.PutLock(ctx, "transaction", Hash(state), Hash(binding), data, s.now().Add(config.OAuthTransactionSeconds*time.Second)); e != nil {
		err = ErrStorage
		return
	}
	location = p.AuthorizationURL(state, tx.Verifier, tx.Nonce)
	return
}

func (s *Service) OAuthCallback(ctx context.Context, state, binding, code string, denied bool) (string, error) {
	if state == "" || binding == "" || len(state) > 128 || len(binding) > 128 {
		return "", ErrState
	}
	data, err := s.Store.TakeLock(ctx, "transaction", Hash(state), Hash(binding), s.now())
	if errors.Is(err, db.ErrInvalidAuthEntry) {
		return "", ErrState
	}
	if err != nil {
		return "", ErrStorage
	}
	if denied {
		return "", ErrDenied
	}
	if code == "" {
		return "", identity.ErrCredential
	}
	var tx transaction
	if json.Unmarshal(data, &tx) != nil {
		return "", ErrStorage
	}
	p, ok := s.Providers[tx.Provider]
	if !ok {
		return "", ErrProvider
	}
	cred, err := p.Exchange(ctx, code, tx.Verifier, tx.Nonce)
	if err != nil {
		return "", err
	}
	issued := s.now()
	tokens, err := s.Identity.SignIn(ctx, cred)
	if err != nil {
		return "", err
	}
	if tokens.LocalID == "" {
		return "", identity.ErrUpstream
	}
	if _, err := s.Store.UpsertUser(ctx, tokens.LocalID); err != nil {
		return "", ErrStorage
	}
	lc := Random()
	data, _ = json.Marshal(login{ReturnTo: tx.ReturnTo, Tokens: tokens, ExpiresAt: issued.Add(time.Duration(tokens.ExpiresIn) * time.Second)})
	if err = s.Store.PutLock(ctx, "login", Hash(lc), "", data, s.now().Add(config.LoginCodeSeconds*time.Second)); err != nil {
		return "", ErrStorage
	}
	return lc, nil
}

func (s *Service) OAuthExchange(ctx context.Context, code string) (OAuthExchangeResult, error) {
	if code == "" || len(code) > 128 {
		return OAuthExchangeResult{}, db.ErrInvalidAuthEntry
	}
	data, err := s.Store.TakeLock(ctx, "login", Hash(code), "", s.now())
	if err != nil {
		if errors.Is(err, db.ErrInvalidAuthEntry) {
			return OAuthExchangeResult{}, db.ErrInvalidAuthEntry
		}
		return OAuthExchangeResult{}, ErrStorage
	}
	var result login
	if json.Unmarshal(data, &result) != nil {
		return OAuthExchangeResult{}, ErrStorage
	}
	result.Tokens.ExpiresIn = int64(result.ExpiresAt.Sub(s.now()) / time.Second)
	if result.Tokens.ExpiresIn <= 0 {
		return OAuthExchangeResult{}, db.ErrInvalidAuthEntry
	}
	if result.ReturnTo == "" {
		result.ReturnTo = "/"
	}
	return OAuthExchangeResult{Tokens: result.Tokens, ReturnTo: result.ReturnTo}, nil
}
