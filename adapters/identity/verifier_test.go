package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type verifierRoundTripFunc func(*http.Request) (*http.Response, error)

func (f verifierRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func signedToken(t *testing.T, key *rsa.PrivateKey, kid string, c jwt.MapClaims, method jwt.SigningMethod) string {
	t.Helper()
	token := jwt.NewWithClaims(method, c)
	token.Header["kid"] = kid
	var signingKey any = key
	if method == jwt.SigningMethodHS256 {
		signingKey = []byte("secret")
	}
	raw, err := token.SignedString(signingKey)
	require.NoError(t, err)
	return raw
}
func TestGCIPVerifier(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	public := func(k *rsa.PrivateKey) string {
		der, e := x509.MarshalPKIXPublicKey(&k.PublicKey)
		require.NoError(t, e)
		return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	}
	var requests atomic.Int64
	var failing atomic.Bool
	var rotated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if failing.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=120")
		keys := map[string]string{"one": public(key)}
		if rotated.Load() {
			keys = map[string]string{"two": public(other)}
		}
		json.NewEncoder(w).Encode(keys)
	}))
	defer server.Close()
	now := time.Now().Truncate(time.Second)
	v := NewGCIPVerifier("project")
	v.now = func() time.Time { return now }
	v.client.Transport = verifierRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, secureTokenKeysURL, r.URL.String())
		req := r.Clone(r.Context())
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})
	claims := func() jwt.MapClaims {
		return jwt.MapClaims{"aud": "project", "iss": "https://securetoken.google.com/project", "sub": "member", "exp": now.Add(time.Hour).Unix(), "iat": now.Add(-time.Minute).Unix(), "auth_time": now.Add(-time.Minute).Unix()}
	}
	raw := signedToken(t, key, "one", claims(), jwt.SigningMethodRS256)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := v.Verify(context.Background(), raw); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.EqualValues(t, 1, requests.Load())
	for _, tc := range []struct {
		name, field string
		value       any
		remove      bool
	}{
		{"audience", "aud", "wrong", false}, {"issuer", "iss", "wrong", false}, {"expired", "exp", now.Unix(), false},
		{"no expiry", "exp", nil, true}, {"no issued", "iat", nil, true}, {"future issued", "iat", now.Add(time.Minute).Unix(), false},
		{"no auth", "auth_time", nil, true}, {"future auth", "auth_time", now.Add(time.Minute).Unix(), false},
		{"long subject", "sub", strings.Repeat("x", 129), false},
		{"no audience", "aud", nil, true},
		{"no issuer", "iss", nil, true},
		{"empty subject", "sub", "", false}, {"no subject", "sub", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := claims()
			if tc.remove {
				delete(c, tc.field)
			} else {
				c[tc.field] = tc.value
			}
			_, e := v.Verify(context.Background(), signedToken(t, key, "one", c, jwt.SigningMethodRS256))
			require.ErrorIs(t, e, ErrToken)
		})
	}
	_, err = v.Verify(context.Background(), signedToken(t, other, "one", claims(), jwt.SigningMethodRS256))
	require.ErrorIs(t, err, ErrToken)
	_, err = v.Verify(context.Background(), signedToken(t, key, "one", claims(), jwt.SigningMethodHS256))
	require.ErrorIs(t, err, ErrToken)
	unknown := signedToken(t, key, "unknown", claims(), jwt.SigningMethodRS256)
	for range 20 {
		_, err = v.Verify(context.Background(), unknown)
		require.ErrorIs(t, err, ErrToken)
	}
	require.EqualValues(t, 2, requests.Load())
	rotated.Store(true)
	now = now.Add(61 * time.Second)
	_, err = v.Verify(context.Background(), signedToken(t, other, "two", claims(), jwt.SigningMethodRS256))
	require.NoError(t, err)
	require.EqualValues(t, 3, requests.Load())
	failing.Store(true)
	_, err = v.Verify(context.Background(), signedToken(t, other, "two", claims(), jwt.SigningMethodRS256))
	require.NoError(t, err)
	now = now.Add(121 * time.Second)
	for range 20 {
		_, err = v.Verify(context.Background(), signedToken(t, other, "two", claims(), jwt.SigningMethodRS256))
		require.ErrorIs(t, err, ErrKeysUnavailable)
	}
	require.EqualValues(t, 4, requests.Load())
	failing.Store(false)
	now = now.Add(2 * time.Second)
	_, err = v.Verify(context.Background(), signedToken(t, other, "two", claims(), jwt.SigningMethodRS256))
	require.NoError(t, err)
}

func TestVerifierCacheDefaultsAndFailures(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	for _, header := range []string{"", "public", "max-age=invalid", "max-age=-1", "max-age=0"} {
		t.Run(header, func(t *testing.T) {
			now := time.Now()
			v := NewGCIPVerifier("project")
			v.now = func() time.Time { return now }
			count := 0
			v.client.Transport = verifierRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				count++
				body, _ := json.Marshal(map[string]string{"key": cert})
				return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": []string{header}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})
			_, err := v.key(context.Background(), "key")
			require.NoError(t, err)
			ttl := 5 * time.Minute
			if header == "max-age=0" {
				ttl = 0
			}
			require.Equal(t, now.Add(ttl), v.expires)
			now = now.Add(ttl)
			_, err = v.key(context.Background(), "key")
			require.NoError(t, err)
			require.Equal(t, 2, count)
		})
	}
	for _, body := range []string{"invalid", "{}", `{"key":"bad certificate"}`} {
		v := NewGCIPVerifier("project")
		v.client.Transport = verifierRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		_, err = v.key(context.Background(), "key")
		require.ErrorIs(t, err, ErrKeysUnavailable)
	}
}
