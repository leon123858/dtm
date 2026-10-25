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

type transaction struct{ Provider, ReturnTo, SessionID string }
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

type service struct {
	cancel                        context.CancelFunc
	done                          chan struct{}
	store                         db.AuthDBWrapper
	providers                     map[string]identity.Provider
	identity                      identity.Identity[identity.GCIPCredential]
	passwordIdentity              identity.Identity[identity.PasswordCredential]
	passwordEmail, passwordSecret string
	clock                         func() time.Time
}

// PasswordLogin uses only the configured E2E account. A nil password identity disables it.
func (s *service) PasswordLogin(ctx context.Context) (identity.Tokens, error) {
	if s.passwordIdentity == nil {
		return identity.Tokens{}, ErrUnauthenticated
	}
	tokens, err := s.passwordIdentity.SignIn(ctx, identity.Credential[identity.PasswordCredential]{Data: identity.PasswordCredential{Email: s.passwordEmail, Password: s.passwordSecret}})
	if err != nil {
		return identity.Tokens{}, err
	}
	if tokens.LocalID == "" {
		return identity.Tokens{}, identity.ErrUpstream
	}
	if _, err := s.store.UpsertUser(ctx, tokens.LocalID); err != nil {
		return identity.Tokens{}, ErrStorage
	}
	return tokens, nil
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Hash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func (s *service) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func (s *service) OAuthStart(ctx context.Context, provider, returnTo string) (state, binding, location string, err error) {
	returnTo, err = validateReturnTo(returnTo)
	if err != nil {
		return
	}
	p, ok := s.providers[provider]
	if !ok {
		err = ErrProvider
		return
	}
	binding = random()
	tx := transaction{Provider: provider, ReturnTo: returnTo}
	authorization, e := p.Start(ctx)
	if e != nil {
		err = e
		return
	}
	if authorization.State == "" || len(authorization.State) > 4096 || authorization.SessionID == "" {
		err = identity.ErrUpstream
		return
	}
	state, location, tx.SessionID = authorization.State, authorization.URL, authorization.SessionID
	data, _ := json.Marshal(tx)
	if e := s.store.PutLock(ctx, "transaction", Hash(state), Hash(binding), data, s.now().Add(config.OAuthTransactionSeconds*time.Second)); e != nil {
		err = ErrStorage
		return
	}
	return
}

func (s *service) OAuthCallback(ctx context.Context, state, binding, code string, denied bool) (string, error) {
	if state == "" || binding == "" || len(state) > 4096 || len(binding) > 128 {
		return "", ErrState
	}
	data, err := s.store.TakeLock(ctx, "transaction", Hash(state), Hash(binding), s.now())
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
	p, ok := s.providers[tx.Provider]
	if !ok {
		return "", ErrProvider
	}
	cred, err := p.Exchange(ctx, identity.OAuthCallback{Code: code, State: state, SessionID: tx.SessionID})
	if err != nil {
		return "", err
	}
	issued := s.now()
	tokens, err := s.identity.SignIn(ctx, cred)
	if err != nil {
		return "", err
	}
	if tokens.LocalID == "" {
		return "", identity.ErrUpstream
	}
	if _, err := s.store.UpsertUser(ctx, tokens.LocalID); err != nil {
		return "", ErrStorage
	}
	lc := random()
	data, _ = json.Marshal(login{ReturnTo: tx.ReturnTo, Tokens: tokens, ExpiresAt: issued.Add(time.Duration(tokens.ExpiresIn) * time.Second)})
	if err = s.store.PutLock(ctx, "login", Hash(lc), "", data, s.now().Add(config.LoginCodeSeconds*time.Second)); err != nil {
		return "", ErrStorage
	}
	return lc, nil
}

func (s *service) OAuthExchange(ctx context.Context, code string) (OAuthExchangeResult, error) {
	if code == "" || len(code) > 128 {
		return OAuthExchangeResult{}, db.ErrInvalidAuthEntry
	}
	data, err := s.store.TakeLock(ctx, "login", Hash(code), "", s.now())
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
	return OAuthExchangeResult{Tokens: result.Tokens, ReturnTo: result.ReturnTo}, nil
}
