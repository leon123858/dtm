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

func (f passwordFunc) SignIn(ctx context.Context, email, password string) (identity.Tokens, error) {
	return f(ctx, email, password)
}

type brokenPasswordStore struct{ db.AuthDBWrapper }

func (brokenPasswordStore) UpsertUser(context.Context, string) (domain.User, error) {
	return domain.User{}, errors.New("storage")
}

func TestPasswordLogin(t *testing.T) {
	ctx := context.Background()
	s, err := NewService(ctx, config.Auth{E2EAuthEnabled: true, E2EAuthEmail: "test@example.com", E2EAuthPassword: " secret "}, mem.NewInMemoryAuthDBWrapper())
	require.NoError(t, err)
	defer s.Close()
	require.NotNil(t, s.PasswordProvider)
	s.PasswordProvider = passwordFunc(func(_ context.Context, email, password string) (identity.Tokens, error) {
		require.Equal(t, "test@example.com", email)
		require.Equal(t, " secret ", password)
		return identity.Tokens{LocalID: "gcid", IDToken: "jwt", RefreshToken: "refresh", ExpiresIn: 3600}, nil
	})
	_, err = s.PasswordLogin(ctx)
	require.NoError(t, err)
	id, err := s.Store.UserGCID2UserId(ctx, "gcid")
	require.NoError(t, err)
	_, err = s.PasswordLogin(ctx)
	require.NoError(t, err)
	again, err := s.Store.UserGCID2UserId(ctx, "gcid")
	require.NoError(t, err)
	require.Equal(t, id, again)
	s.Store = brokenPasswordStore{}
	tokens, err := s.PasswordLogin(ctx)
	require.ErrorIs(t, err, ErrStorage)
	require.Zero(t, tokens)
	s.PasswordProvider = passwordFunc(func(context.Context, string, string) (identity.Tokens, error) {
		return identity.Tokens{}, identity.ErrCredential
	})
	_, err = s.PasswordLogin(ctx)
	require.ErrorIs(t, err, identity.ErrCredential)
	s.PasswordProvider = passwordFunc(func(context.Context, string, string) (identity.Tokens, error) {
		return identity.Tokens{IDToken: "jwt"}, nil
	})
	_, err = s.PasswordLogin(ctx)
	require.ErrorIs(t, err, identity.ErrUpstream)
	s.PasswordProvider = nil
	_, err = s.PasswordLogin(ctx)
	require.ErrorIs(t, err, ErrUnauthenticated)
}
