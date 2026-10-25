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
)

// NewService initializes authentication and starts cleanup until ctx is canceled
// or Close is called. The caller retains ownership of the database.
func NewService(ctx context.Context, cfg config.Auth, store db.AuthDBWrapper) (*Service, error) {
	if store == nil {
		return nil, errors.New("auth store is required")
	}
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	provider, err := identity.NewGoogle(ctx, cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.RedirectURL, client)
	if err != nil {
		return nil, errors.New("cannot initialize Google login")
	}
	cleanupCtx, cancel := context.WithCancel(ctx)
	s := &Service{
		Store:     store,
		Providers: map[string]identity.Provider{"google": provider},
		Identity:  identity.NewGCIP(client, cfg.APIKey, cfg.RedirectURL),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	if cfg.E2EAuthEnabled {
		s.PasswordProvider = identity.NewEmailPassword(client, cfg.APIKey)
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
func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

func (s *Service) cleanup(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticks:
			cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if s.Store.CleanupLock(cleanupCtx, now) != nil {
				slog.Warn("auth failed", "stage", "cleanup", "code", ErrStorage.Error())
			}
			cancel()
		}
	}
}
