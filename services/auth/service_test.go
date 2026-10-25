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
	t.Setenv("GOOGLE_CLIENT_ID", "client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("GCIP_API_KEY", "key")
	t.Setenv("GCIP_PROJECT_ID", "project")
	cfg, err := config.LoadAuth(false, "8080")
	require.NoError(t, err)
	s, err := NewService(context.Background(), cfg, store)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	require.Same(t, store, s.Store)
	google, ok := s.Providers["google"].(*identity.Google)
	require.True(t, ok)
	require.Equal(t, "https://example.com/backend/auth/callback", google.Config.RedirectURL)
	require.Equal(t, google.Config.RedirectURL, s.Identity.(*identity.GCIP).RedirectURL)
	require.Equal(t, 10*time.Second, google.Client.Timeout)
	require.ErrorIs(t, google.Client.CheckRedirect(nil, nil), http.ErrUseLastResponse)
	require.NotNil(t, s.Identity)
	s.Close()
	select {
	case <-s.done:
	default:
		t.Fatal("Close returned before cleanup stopped")
	}
	s.Close()
	(&Service{}).Close()
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
	s := &Service{Store: store}
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
