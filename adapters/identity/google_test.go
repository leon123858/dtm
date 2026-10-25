package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGooglePKCEAndCredentialValidation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encode := func(v any) string {
		b, e := json.Marshal(v)
		require.NoError(t, e)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	claims := map[string]any{"iss": "https://accounts.google.com", "aud": "client", "sub": "subject", "exp": time.Now().Add(time.Hour).Unix(), "nonce": "nonce"}
	verifier := oauth2.GenerateVerifier()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/certs" {
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "alg": "RS256", "kid": "test", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
			return
		}
		require.NoError(t, r.ParseForm())
		require.Equal(t, verifier, r.Form.Get("code_verifier"))
		require.Equal(t, "code", r.Form.Get("code"))
		unsigned := encode(map[string]string{"alg": "RS256", "kid": "test"}) + "." + encode(claims)
		sum := sha256.Sum256([]byte(unsigned))
		signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		require.NoError(t, e)
		json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)})
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "www.googleapis.com" {
			copy := r.Clone(r.Context())
			u, _ := url.Parse(server.URL + "/certs")
			copy.URL = u
			return transport.RoundTrip(copy)
		}
		return transport.RoundTrip(r)
	})
	g, err := NewGoogle(context.Background(), "client", "secret", "http://localhost/callback", client)
	require.NoError(t, err)
	g.Config.Endpoint.TokenURL = server.URL + "/token"
	u, err := url.Parse(g.AuthorizationURL("state", verifier, "nonce"))
	require.NoError(t, err)
	require.Equal(t, "S256", u.Query().Get("code_challenge_method"))
	require.Equal(t, oauth2.S256ChallengeFromVerifier(verifier), u.Query().Get("code_challenge"))
	require.Equal(t, "nonce", u.Query().Get("nonce"))
	require.Equal(t, "select_account", u.Query().Get("prompt"))
	credential, err := g.Exchange(context.Background(), "code", verifier, "nonce")
	require.NoError(t, err)
	require.Equal(t, "google.com", credential.ProviderID)
	for _, field := range []string{"iss", "aud", "nonce", "exp"} {
		old := claims[field]
		if field == "exp" {
			claims[field] = time.Now().Add(-time.Hour).Unix()
		} else {
			claims[field] = "wrong"
		}
		_, err = g.Exchange(context.Background(), "code", verifier, "nonce")
		require.ErrorIs(t, err, ErrCredential)
		claims[field] = old
	}
}
