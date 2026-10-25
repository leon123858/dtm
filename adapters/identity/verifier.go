package identity

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
)

const secureTokenKeysURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"

var ErrToken = errors.New("invalid_id_token")
var ErrKeysUnavailable = errors.New("signing_keys_unavailable")

type TokenVerifier interface {
	Verify(context.Context, string) (string, error)
}

// GCIPVerifier caches only unexpired Google Secure Token certificates.
// The mutex coalesces refreshes, including failed requests via a short retry delay.
type GCIPVerifier struct {
	project                       string
	client                        *http.Client
	now                           func() time.Time
	mu                            sync.Mutex
	keys                          map[string]*rsa.PublicKey
	expires, lastUnknown, retryAt time.Time
}

func NewGCIPVerifier(project string) *GCIPVerifier {
	return &GCIPVerifier{project: project, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}
func (v *GCIPVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	valid := now.Before(v.expires)
	if valid {
		if key := v.keys[kid]; key != nil {
			return key, nil
		}
		if !v.lastUnknown.IsZero() && now.Sub(v.lastUnknown) < time.Minute {
			return nil, ErrToken
		}
		v.lastUnknown = now
	}
	if now.Before(v.retryAt) {
		return nil, ErrKeysUnavailable
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, secureTokenKeysURL, nil)
	resp, err := v.client.Do(req)
	if err != nil {
		v.retryAt = v.now().Add(time.Second)
		return nil, ErrKeysUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		v.retryAt = v.now().Add(time.Second)
		return nil, ErrKeysUnavailable
	}
	certs := map[string]string{}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&certs) != nil || len(certs) == 0 {
		v.retryAt = v.now().Add(time.Second)
		return nil, ErrKeysUnavailable
	}
	keys := make(map[string]*rsa.PublicKey, len(certs))
	for id, cert := range certs {
		key, e := jwt.ParseRSAPublicKeyFromPEM([]byte(cert))
		if e != nil {
			v.retryAt = v.now().Add(time.Second)
			return nil, ErrKeysUnavailable
		}
		keys[id] = key
	}
	ttl := 5 * time.Minute
	for _, part := range strings.Split(resp.Header.Get("Cache-Control"), ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(k, "max-age") {
			if n, e := strconv.ParseInt(strings.Trim(val, "\""), 10, 32); e == nil && n >= 0 {
				ttl = time.Duration(n) * time.Second
			}
		}
	}
	v.keys = keys
	v.expires = v.now().Add(ttl)
	v.retryAt = time.Time{}
	if key := keys[kid]; key != nil {
		return key, nil
	}
	v.lastUnknown = now
	return nil, ErrToken
}

type gcipClaims struct {
	jwt.RegisteredClaims
	AuthTime *jwt.NumericDate `json:"auth_time"`
}

func (v *GCIPVerifier) Verify(ctx context.Context, raw string) (string, error) {
	if v.project == "" {
		return "", ErrToken
	}
	claims := &gcipClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, ErrToken
		}
		return v.key(ctx, kid)
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience(v.project), jwt.WithIssuer("https://securetoken.google.com/"+v.project), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(v.now))
	if errors.Is(err, ErrKeysUnavailable) {
		return "", ErrKeysUnavailable
	}
	if err != nil {
		return "", ErrToken
	}
	now := v.now()
	if claims.IssuedAt == nil || claims.AuthTime == nil || claims.AuthTime.After(now) || claims.AuthTime.Unix() <= 0 || claims.IssuedAt.Unix() <= 0 || strings.TrimSpace(claims.Subject) == "" || !utf8.ValidString(claims.Subject) || utf8.RuneCountInString(claims.Subject) > 128 {
		return "", ErrToken
	}
	return claims.Subject, nil
}
