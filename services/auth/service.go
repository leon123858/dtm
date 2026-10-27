package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/identity"
	"dtm/config"
	"dtm/domain"
	"github.com/google/uuid"
)

// Service exposes authentication, member subscriptions, and lifecycle operations.
type Service interface {
	GetUser(context.Context, uuid.UUID) (domain.User, error)
	UpdateMe(context.Context, *string, *bool) (domain.User, error)
	OAuthStart(context.Context, string, string) (string, string, string, error)
	OAuthCallback(context.Context, string, string, string, bool) (string, error)
	OAuthExchange(context.Context, string) (OAuthExchangeResult, error)
	PasswordLogin(context.Context) (identity.Tokens, error)
	Refresh(context.Context, string) (identity.Tokens, error)
	SubscribedTrips(context.Context, uuid.UUID) ([]domain.SubscribedTrip, error)
	SubscribeTrip(context.Context, uuid.UUID, uuid.UUID) error
	UnsubscribeTrip(context.Context, uuid.UUID, uuid.UUID) error
	TouchSubscribedTrip(context.Context, uuid.UUID, uuid.UUID) error
	Close()
}

// Dependencies supplies adapters and an optional clock at construction time.
// A nil Now uses time.Now. A password identity is used only when E2E auth is enabled.
type Dependencies struct {
	Store            db.AuthDBWrapper
	Providers        map[string]identity.Provider
	Identity         identity.Identity[identity.GCIPCredential]
	PasswordIdentity identity.Identity[identity.PasswordCredential]
	Now              func() time.Time
}

// NewService initializes authentication and starts cleanup until ctx is canceled
// or Close is called. The caller retains ownership of the database.
func NewService(ctx context.Context, cfg config.Auth, store db.AuthDBWrapper) (Service, error) {
	if store == nil {
		return nil, errors.New("auth store is required")
	}
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	gcip := identity.NewGCIP(client, cfg.APIKey, cfg.RedirectURL)
	deps := Dependencies{Store: store, Providers: map[string]identity.Provider{"google": identity.NewGoogle(gcip), "github": identity.NewGitHub(gcip)}, Identity: gcip}
	if cfg.E2EAuthEnabled {
		deps.PasswordIdentity = identity.NewEmailPassword(client, cfg.APIKey)
	}
	return NewServiceWithDependencies(ctx, cfg, deps)
}

// NewServiceWithDependencies starts a service using caller-supplied adapters.
// The caller owns the database and must provide adapters for the operations used.
// Dependencies are captured at construction; Close stops the cleanup worker.
func NewServiceWithDependencies(ctx context.Context, cfg config.Auth, deps Dependencies) (Service, error) {
	if deps.Store == nil {
		return nil, errors.New("auth store is required")
	}
	cleanupCtx, cancel := context.WithCancel(ctx)
	providers := make(map[string]identity.Provider, len(deps.Providers))
	for name, provider := range deps.Providers {
		providers[name] = provider
	}
	s := &service{store: deps.Store, providers: providers, identity: deps.Identity, clock: deps.Now, cancel: cancel, done: make(chan struct{})}
	if cfg.E2EAuthEnabled {
		s.passwordIdentity = deps.PasswordIdentity
		s.passwordEmail, s.passwordSecret = cfg.E2EAuthEmail, cfg.E2EAuthPassword
	}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		s.cleanup(cleanupCtx, ticker.C)
	}()
	return s, nil
}

// Close stops cleanup and waits for any in-flight cleanup to finish.
// It is safe to call repeatedly, including on services assembled directly.
func (s *service) Close() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

func (s *service) cleanup(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticks:
			cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if s.store.CleanupLock(cleanupCtx, now) != nil {
				slog.Warn("auth failed", "stage", "cleanup", "code", ErrStorage.Error())
			}
			cancel()
		}
	}
}

var _ Service = (*service)(nil)

func (s *service) Refresh(ctx context.Context, token string) (identity.Tokens, error) {
	return s.identity.Refresh(ctx, token)
}
