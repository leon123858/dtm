package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/identity"
	"dtm/config"
	"github.com/stretchr/testify/require"
)

func TestNewServiceAndClose(t *testing.T) {
	store := mem.NewInMemoryAuthDBWrapper()
	t.Setenv("FRONTEND_URL", "https://example.com")
	t.Setenv("OAUTH_REDIRECT_PATH", "/backend/auth/callback")
	t.Setenv("GCIP_API_KEY", "key")
	t.Setenv("GCIP_PROJECT_ID", "project")
	cfg, err := config.LoadAuth(false, "8080")
	require.NoError(t, err)
	api, err := NewService(context.Background(), cfg, store)
	require.NoError(t, err)
	s := api.(*service)
	t.Cleanup(s.Close)
	require.Same(t, store, s.store)
	google, ok := s.providers["google"].(*identity.GoogleGCIPProvider)
	require.True(t, ok)
	require.NotNil(t, google)
	_, ok = s.providers["github"].(*identity.GithubGCIPProvider)
	require.True(t, ok)
	gcip := s.identity.(*identity.GCIP)
	require.Equal(t, "https://example.com/backend/auth/callback", gcip.RedirectURL)
	require.Equal(t, 10*time.Second, gcip.Client.Timeout)
	require.ErrorIs(t, gcip.Client.CheckRedirect(nil, nil), http.ErrUseLastResponse)
	s.Close()
	select {
	case <-s.done:
	default:
		t.Fatal("Close returned before cleanup stopped")
	}
	s.Close()
	(&service{}).Close()
}

type cleanupStore struct {
	db.AuthDBWrapper
	started chan context.Context
}

func (s cleanupStore) CleanupLock(ctx context.Context, _ time.Time) error {
	s.started <- ctx
	<-ctx.Done()
	return ctx.Err()
}

func TestCleanupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := cleanupStore{started: make(chan context.Context, 1)}
	s := &service{store: store}
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() { defer close(done); s.cleanup(ctx, ticks) }()
	ticks <- time.Now()
	select {
	case cleanupCtx := <-store.started:
		deadline, ok := cleanupCtx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(10*time.Second), deadline, time.Second)
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not stop after cancellation")
	}
}

type refreshIdentity struct {
	identity.Identity[identity.GCIPCredential]
	refresh func(context.Context, string) (identity.Tokens, error)
}

func (i refreshIdentity) Refresh(ctx context.Context, token string) (identity.Tokens, error) {
	return i.refresh(ctx, token)
}

func TestRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, failure := range []error{nil, identity.ErrRefresh, identity.ErrUpstream} {
		expected := identity.Tokens{}
		if failure == nil {
			expected = identity.Tokens{IDToken: "new-id", RefreshToken: "new-refresh", ExpiresIn: 3600}
		}
		calls := 0
		s, err := NewServiceWithDependencies(ctx, config.Auth{}, Dependencies{
			Store: mem.NewInMemoryAuthDBWrapper(),
			Identity: refreshIdentity{refresh: func(got context.Context, token string) (identity.Tokens, error) {
				calls++
				require.Same(t, ctx, got)
				require.Equal(t, "refresh-token", token)
				return expected, failure
			}},
		})
		require.NoError(t, err)
		t.Cleanup(s.Close)
		result, err := s.Refresh(ctx, "refresh-token")
		require.Equal(t, expected, result)
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, calls)
	}
}
