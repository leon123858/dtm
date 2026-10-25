package auth

import (
	"context"
	"errors"
	"testing"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/identity"
	"dtm/config"
	"dtm/domain"
	"github.com/stretchr/testify/require"
)

type passwordFunc func(context.Context, string, string) (identity.Tokens, error)

func (f passwordFunc) SignIn(ctx context.Context, credential identity.Credential[identity.PasswordCredential]) (identity.Tokens, error) {
	return f(ctx, credential.Data.Email, credential.Data.Password)
}

type brokenPasswordStore struct{ db.AuthDBWrapper }

func (brokenPasswordStore) UpsertUser(context.Context, string) (domain.User, error) {
	return domain.User{}, errors.New("storage")
}

func TestPasswordLogin(t *testing.T) {
	ctx := context.Background()
	store := mem.NewInMemoryAuthDBWrapper()
	cfg := config.Auth{E2EAuthEnabled: true, E2EAuthEmail: "test@example.com", E2EAuthPassword: " secret "}
	newService := func(store db.AuthDBWrapper, provider identity.Identity[identity.PasswordCredential]) Service {
		t.Helper()
		s, err := NewServiceWithDependencies(ctx, cfg, Dependencies{Store: store, PasswordIdentity: provider})
		require.NoError(t, err)
		t.Cleanup(s.Close)
		return s
	}
	provider := passwordFunc(func(_ context.Context, email, password string) (identity.Tokens, error) {
		require.Equal(t, "test@example.com", email)
		require.Equal(t, " secret ", password)
		return identity.Tokens{LocalID: "gcid", IDToken: "jwt", RefreshToken: "refresh", ExpiresIn: 3600}, nil
	})
	s := newService(store, provider)
	_, err := s.PasswordLogin(ctx)
	require.NoError(t, err)
	id, err := store.UserGCID2UserId(ctx, "gcid")
	require.NoError(t, err)
	_, err = s.PasswordLogin(ctx)
	require.NoError(t, err)
	again, err := store.UserGCID2UserId(ctx, "gcid")
	require.NoError(t, err)
	require.Equal(t, id, again)
	s = newService(brokenPasswordStore{}, provider)
	tokens, err := s.PasswordLogin(ctx)
	require.ErrorIs(t, err, ErrStorage)
	require.Zero(t, tokens)
	s = newService(store, passwordFunc(func(context.Context, string, string) (identity.Tokens, error) {
		return identity.Tokens{}, identity.ErrCredential
	}))
	_, err = s.PasswordLogin(ctx)
	require.ErrorIs(t, err, identity.ErrCredential)
	s = newService(store, passwordFunc(func(context.Context, string, string) (identity.Tokens, error) {
		return identity.Tokens{IDToken: "jwt"}, nil
	}))
	_, err = s.PasswordLogin(ctx)
	require.ErrorIs(t, err, identity.ErrUpstream)
	s = newService(store, nil)
	_, err = s.PasswordLogin(ctx)
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func (f passwordFunc) Refresh(context.Context, string) (identity.Tokens, error) {
	return identity.Tokens{}, identity.ErrNotImplemented
}
