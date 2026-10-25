package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type GCIP struct {
	Client                            *http.Client
	APIKey, RedirectURL               string
	SignInURL, RefreshURL, AuthURIURL string
}

func NewGCIP(client *http.Client, key, redirect string) *GCIP {
	return &GCIP{Client: client, APIKey: key, RedirectURL: redirect, AuthURIURL: "https://identitytoolkit.googleapis.com/v1/accounts:createAuthUri", SignInURL: "https://identitytoolkit.googleapis.com/v1/accounts:signInWithIdp", RefreshURL: "https://securetoken.googleapis.com/v1/token"}
}
func transportError(err error) error {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return ErrTimeout
	}
	return ErrUpstream
}
func (g *GCIP) request(ctx context.Context, endpoint, contentType string, body io.Reader, out any, refresh bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?key="+url.QueryEscape(g.APIKey), body)
	if err != nil {
		return ErrUpstream
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := g.Client.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Never return upstream response text: it can contain credentials.
		if refresh && (resp.StatusCode == 400 || resp.StatusCode == 401) {
			return ErrRefresh
		}
		return ErrUpstream
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(out) != nil {
		return ErrUpstream
	}
	return nil
}
func checkedTokens(id, refresh, expiry string) (Tokens, error) {
	seconds, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || seconds <= 0 || seconds > 86400 || id == "" || refresh == "" {
		return Tokens{}, ErrUpstream
	}
	return Tokens{IDToken: id, RefreshToken: refresh, ExpiresIn: seconds}, nil
}
func (g *GCIP) SignIn(ctx context.Context, credential Credential[GCIPCredential]) (Tokens, error) {
	cred := credential.Data
	if cred.ProviderID == "" || cred.RequestURI == "" || cred.SessionID == "" {
		return Tokens{}, ErrCredential
	}
	request := map[string]any{"requestUri": cred.RequestURI, "sessionId": cred.SessionID, "returnSecureToken": true}
	body, _ := json.Marshal(request)
	var result struct {
		IDToken          string `json:"idToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresIn        string `json:"expiresIn"`
		LocalID          string `json:"localId"`
		ProviderID       string `json:"providerId"`
		NeedConfirmation bool   `json:"needConfirmation"`
	}
	if err := g.request(ctx, g.SignInURL, "application/json", bytes.NewReader(body), &result, false); err != nil {
		return Tokens{}, err
	}
	if result.NeedConfirmation || result.ProviderID != cred.ProviderID {
		return Tokens{}, ErrCredential
	}
	if result.LocalID == "" {
		return Tokens{}, ErrUpstream
	}
	tokens, err := checkedTokens(result.IDToken, result.RefreshToken, result.ExpiresIn)
	if err != nil {
		return Tokens{}, err
	}
	tokens.LocalID = result.LocalID
	return tokens, nil
}
func (g *GCIP) Refresh(ctx context.Context, token string) (Tokens, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}}
	var result struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    string `json:"expires_in"`
	}
	if err := g.request(ctx, g.RefreshURL, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), &result, true); err != nil {
		return Tokens{}, err
	}
	return checkedTokens(result.IDToken, result.RefreshToken, result.ExpiresIn)
}
