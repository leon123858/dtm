package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/identity"
	"dtm/config"
	"dtm/domain"

	"github.com/stretchr/testify/require"
)

func TestValidateReturnTo(t *testing.T) {
	for _, input := range []string{"", "/", "/history", "/trip/550e8400-e29b-41d4-a716-446655440000?tab=costs#record", "/history?q=a%20b&discount=100%25#中文", "/?next=https%3A%2F%2Fexample.com"} {
		got, err := validateReturnTo(input)
		require.NoError(t, err, input)
		if input == "" {
			input = "/"
		}
		require.Equal(t, input, got)
	}
	for _, input := range []string{
		"https://example.com", "//example.com", "///history", "history", " ", "/auth/complete", "/backend/auth/google", "/trip/abc123", "/history/", "/history/../", "/./history", "/trip//", "/%68istory", "/%2fhistory", "/%252fhistory", "/history%3fignored", "/history\\", "/history\n", "/history?q=%0a", "/history?q=%255c", "/history?q=%25&x=%255c", "/history#%250d", "/history?bad=%zz", "/history#%zz", "/history?q=%7f",
	} {
		_, err := validateReturnTo(input)
		require.ErrorIs(t, err, ErrRequest, input)
	}
}

func TestInvalidReturnToDoesNotStartTransaction(t *testing.T) {
	// Nil dependencies would panic if validation touched storage or a provider.
	s := &service{}
	state, binding, location, err := s.OAuthStart(context.Background(), "google", "//evil.example")
	require.ErrorIs(t, err, ErrRequest)
	require.Empty(t, state)
	require.Empty(t, binding)
	require.Empty(t, location)
}

func TestLoginRemainingLifetime(t *testing.T) {
	now := time.Now()
	s := &service{store: mem.NewInMemoryAuthDBWrapper(), clock: func() time.Time { return now }}
	data, err := json.Marshal(login{
		Tokens:    identity.Tokens{IDToken: "test", ExpiresIn: 3600},
		ExpiresAt: now.Add(time.Hour),
		ReturnTo:  "/history",
	})
	require.NoError(t, err)
	require.NoError(t, s.store.PutLock(context.Background(), "login", Hash("login-code"), "", data, now.Add(time.Minute)))
	now = now.Add(time.Second)
	result, err := s.OAuthExchange(context.Background(), "login-code")
	require.NoError(t, err)
	require.Equal(t, "/history", result.ReturnTo)
	require.Equal(t, int64(3599), result.ExpiresIn)
}

func TestReturnToResourceLimits(t *testing.T) {
	const prefix = "/history?q="
	atLimit := prefix + strings.Repeat("a", maxReturnToBytes-len(prefix))
	got, err := validateReturnTo(atLimit)
	require.NoError(t, err)
	require.Equal(t, atLimit, got)
	nested := func(depth int) string { return prefix + "%" + strings.Repeat("25", depth-1) + "41" }
	got, err = validateReturnTo(nested(maxReturnToDecodeDepth))
	require.NoError(t, err)
	require.Equal(t, nested(maxReturnToDecodeDepth), got)
	for _, input := range []string{
		atLimit + "a",
		nested(maxReturnToDecodeDepth + 1),
		strings.Replace(nested(maxReturnToDecodeDepth+1), "?q=", "#", 1),
		nested(30000), // Reviewer regression: ~60 KB quadratic decoding input.
	} {
		// Rejection must happen before any storage or provider access.
		s := &service{}
		state, binding, location, err := s.OAuthStart(context.Background(), "google", input)
		require.ErrorIs(t, err, ErrRequest)
		require.Empty(t, state)
		require.Empty(t, binding)
		require.Empty(t, location)
	}
}

type profileIdentity struct{ tokens *identity.Tokens }

func (i profileIdentity) SignIn(context.Context, identity.Credential[identity.GCIPCredential]) (identity.Tokens, error) {
	return *i.tokens, nil
}
func (i profileIdentity) Refresh(context.Context, string) (identity.Tokens, error) {
	return *i.tokens, nil
}

type profileProvider struct{}

func (profileProvider) Start(context.Context) (identity.Authorization, error) {
	return identity.Authorization{State: "state", SessionID: "session", URL: "https://example.com"}, nil
}
func (profileProvider) Exchange(context.Context, identity.OAuthCallback) (identity.Credential[identity.GCIPCredential], error) {
	return identity.Credential[identity.GCIPCredential]{}, nil
}
func TestLoginSynchronizesEmailOnly(t *testing.T) {
	for _, mode := range []string{"oauth", "password"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			store := mem.NewInMemoryAuthDBWrapper()
			tokens := identity.Tokens{LocalID: "same-gcid", Email: "first@example.com", IDToken: "token", RefreshToken: "refresh", ExpiresIn: 3600}
			s := &service{store: store, identity: profileIdentity{&tokens}, providers: map[string]identity.Provider{"google": profileProvider{}}, passwordIdentity: passwordFunc(func(context.Context, string, string) (identity.Tokens, error) { return tokens, nil })}
			login := func() {
				t.Helper()
				if mode == "password" {
					_, e := s.PasswordLogin(ctx)
					require.NoError(t, e)
				} else {
					state, binding, _, e := s.OAuthStart(ctx, "google", "/")
					require.NoError(t, e)
					_, e = s.OAuthCallback(ctx, state, binding, "code", false)
					require.NoError(t, e)
				}
			}
			login()
			id, e := store.UserGCID2UserId(ctx, tokens.LocalID)
			require.NoError(t, e)
			u, e := store.GetUser(ctx, id)
			require.NoError(t, e)
			require.Equal(t, tokens.Email, *u.Email)
			require.Nil(t, u.Name)
			name, sub := "  My name  ", true
			_, e = s.UpdateMe(domain.WithUserID(ctx, id), &name, &sub)
			require.NoError(t, e)
			tokens.Email = "second@example.com"
			login()
			u, e = store.GetUser(ctx, id)
			require.NoError(t, e)
			require.Equal(t, tokens.Email, *u.Email)
			require.Equal(t, name, *u.Name)
			require.True(t, u.SubMail)
			tokens.Email = ""
			login()
			u, e = store.GetUser(ctx, id)
			require.NoError(t, e)
			require.Equal(t, "second@example.com", *u.Email)
			require.Equal(t, name, *u.Name)
			require.True(t, u.SubMail)
			_, e = s.UpdateMe(ctx, &name, nil)
			require.ErrorIs(t, e, domain.ErrUnauthenticated)
		})
	}
}

type passwordFunc func(context.Context, string, string) (identity.Tokens, error)

func (f passwordFunc) SignIn(ctx context.Context, credential identity.Credential[identity.PasswordCredential]) (identity.Tokens, error) {
	return f(ctx, credential.Data.Email, credential.Data.Password)
}

type brokenPasswordStore struct{ db.AuthDBWrapper }

func (brokenPasswordStore) UpsertUser(context.Context, string, ...string) (domain.User, error) {
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
