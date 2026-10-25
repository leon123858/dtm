package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthConfig(t *testing.T) {
	t.Setenv("GCIP_PROJECT_ID", "project")
	for _, key := range []string{"FRONTEND_URL", "OAUTH_REDIRECT_PATH", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GCIP_API_KEY"} {
		t.Setenv(key, "")
	}
	_, err := LoadAuth(false, "8081")
	require.EqualError(t, err, "invalid FRONTEND_URL")
	for _, key := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GCIP_API_KEY"} {
		t.Setenv(key, "configured")
	}
	for _, dev := range []bool{true, false} {
		t.Setenv("FRONTEND_URL", "https://frontend.example/")
		for _, invalid := range []string{"", "/", "callback", "https://backend.example/auth/callback", "//evil.example/callback", "/auth/../callback", "/./callback", "/auth//callback", "/auth/callback/", "/auth/%63allback", "/auth/%252fcallback", "/auth/callback?", "/auth/callback?q=1", "/auth/callback#", "/auth/callback#fragment", "/auth/\\callback", "/auth/callback\n", "/auth/call back", "/auth/call;back"} {
			t.Setenv("OAUTH_REDIRECT_PATH", invalid)
			_, err := LoadAuth(dev, "8081")
			require.EqualError(t, err, "invalid OAUTH_REDIRECT_PATH", invalid)
		}
		for _, valid := range []string{"/backend/auth/callback", "/auth/callback", "/custom-proxy/auth/callback"} {
			t.Setenv("OAUTH_REDIRECT_PATH", valid)
			c, err := LoadAuth(dev, "8081")
			require.NoError(t, err)
			require.Equal(t, valid, c.RedirectPath)
			require.Equal(t, "https://frontend.example"+valid, c.RedirectURL)
		}
	}
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	t.Setenv("OAUTH_REDIRECT_PATH", "/backend/auth/callback")
	c, err := LoadAuth(true, "8081")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000/backend/auth/callback", c.RedirectURL)
	_, err = LoadAuth(false, "8081")
	require.EqualError(t, err, "invalid FRONTEND_URL")
	t.Setenv("FRONTEND_URL", "https://example.com/path")
	_, err = LoadAuth(false, "8081")
	require.EqualError(t, err, "invalid FRONTEND_URL")
}

func TestFrontendURL(t *testing.T) {
	t.Setenv("FRONTEND_URL", "")
	require.Equal(t, DEFAULT_FRONTEND_URL, FrontendURL())
	require.NoError(t, os.Unsetenv("FRONTEND_URL"))
	require.Equal(t, DEFAULT_FRONTEND_URL, FrontendURL())
	t.Setenv("FRONTEND_URL", "https://frontend.example/")
	require.Equal(t, "https://frontend.example", FrontendURL())
}

func TestAuthConfigRequiresEveryCredential(t *testing.T) {
	t.Setenv("GCIP_PROJECT_ID", "project")
	for _, dev := range []bool{true, false} {
		for _, missing := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GCIP_API_KEY"} {
			t.Run(fmt.Sprintf("dev=%t/%s", dev, missing), func(t *testing.T) {
				t.Setenv("FRONTEND_URL", "https://frontend.example")
				t.Setenv("OAUTH_REDIRECT_PATH", "/backend/auth/callback")
				for _, key := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GCIP_API_KEY"} {
					t.Setenv(key, "configured")
				}
				t.Setenv(missing, "")
				_, err := LoadAuth(dev, "8080")
				require.EqualError(t, err, "GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET and GCIP_API_KEY must all be set")
			})
		}
	}
}

func TestDevEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("AUTH_TEST_EXISTING", "injected")
	require.NoError(t, os.WriteFile(filepath.Join(".env"), []byte("AUTH_TEST_EXISTING=file\nAUTH_TEST_NEW=file\n"), 0600))
	os.Unsetenv("AUTH_TEST_NEW")
	t.Cleanup(func() { os.Unsetenv("AUTH_TEST_NEW") })
	require.NoError(t, LoadDevEnv(false))
	require.Empty(t, os.Getenv("AUTH_TEST_NEW"))
	require.NoError(t, LoadDevEnv(true))
	require.Equal(t, "injected", os.Getenv("AUTH_TEST_EXISTING"))
	require.Equal(t, "file", os.Getenv("AUTH_TEST_NEW"))
	require.NoError(t, os.WriteFile(".env", []byte("BAD='secret"), 0600))
	err := LoadDevEnv(true)
	require.EqualError(t, err, "cannot load .env")
}

func TestAuthConfigRequiresProject(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://frontend.example")
	t.Setenv("GCIP_PROJECT_ID", " ")
	_, err := LoadAuth(false, "8080")
	require.EqualError(t, err, "GCIP_PROJECT_ID must be set")
}

func TestE2EAuthConfig(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://example.com")
	t.Setenv("OAUTH_REDIRECT_PATH", "/auth/callback")
	for _, key := range []string{"GCIP_PROJECT_ID", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GCIP_API_KEY"} {
		t.Setenv(key, "configured")
	}
	for _, tc := range []struct {
		enabled, email, password string
		valid, on                bool
	}{
		{"", "", "", true, false}, {"false", "", "", true, false},
		{"bad", "email", "secret", false, false},
		{"true", "", "secret", false, true}, {"true", "email", "", false, true},
		{"true", " ", "secret", false, true}, {"true", "email", " \t", false, true},
		{"true", "email", " secret ", true, true},
	} {
		t.Setenv("E2E_AUTH_ENABLED", tc.enabled)
		t.Setenv("E2E_AUTH_EMAIL", tc.email)
		t.Setenv("E2E_AUTH_PASSWORD", tc.password)
		cfg, err := LoadAuth(false, "8080")
		if !tc.valid {
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tc.on, cfg.E2EAuthEnabled)
		if tc.on {
			require.Equal(t, tc.password, cfg.E2EAuthPassword)
		}
	}
}
