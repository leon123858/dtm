package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
)

// GoogleGCIPProvider handles Google authorization through GCIP.
type GoogleGCIPProvider struct{ identity *GCIP }

// GithubGCIPProvider handles GitHub authorization through GCIP.
type GithubGCIPProvider struct{ identity *GCIP }

func NewGoogle(gcip *GCIP) *GoogleGCIPProvider { return &GoogleGCIPProvider{identity: gcip} }
func NewGitHub(gcip *GCIP) *GithubGCIPProvider { return &GithubGCIPProvider{identity: gcip} }
func (p *GoogleGCIPProvider) Start(ctx context.Context) (Authorization, error) {
	body := map[string]any{"providerId": "google.com", "continueUri": p.identity.RedirectURL, "oauthScope": "openid email profile"}
	body["authFlowType"] = "CODE_FLOW"
	body["customParameter"] = map[string]string{"prompt": "select_account"}

	raw, _ := json.Marshal(body)
	var result struct {
		AuthURI   string `json:"authUri"`
		SessionID string `json:"sessionId"`
	}
	if err := p.identity.request(ctx, p.identity.AuthURIURL, "application/json", bytes.NewReader(raw), &result, false); err != nil {
		return Authorization{}, err
	}
	uri, err := url.Parse(result.AuthURI)
	if err != nil || uri.Scheme != "https" || uri.Host != "accounts.google.com" || uri.User != nil || uri.Fragment != "" || result.SessionID == "" {
		return Authorization{}, ErrUpstream
	}
	if uri.Path != "/o/oauth2/auth" && uri.Path != "/o/oauth2/v2/auth" {
		return Authorization{}, ErrUpstream
	}
	query, err := url.ParseQuery(uri.RawQuery)
	if err != nil || len(query["state"]) != 1 || query.Get("state") == "" || len(query.Get("state")) > 4096 {
		return Authorization{}, ErrUpstream
	}
	return Authorization{URL: result.AuthURI, State: query.Get("state"), SessionID: result.SessionID}, nil
}

// Exchange prepares the authorization response; GCIP performs the token exchange.
func (p *GoogleGCIPProvider) Exchange(_ context.Context, callback OAuthCallback) (Credential[GCIPCredential], error) {
	if callback.SessionID == "" || callback.State == "" || callback.Code == "" {
		return Credential[GCIPCredential]{}, ErrCredential
	}
	uri, err := url.Parse(p.identity.RedirectURL)
	if err != nil {
		return Credential[GCIPCredential]{}, ErrUpstream
	}
	query := uri.Query()
	query.Set("state", callback.State)
	query.Set("code", callback.Code)
	uri.RawQuery = query.Encode()
	return Credential[GCIPCredential]{Data: GCIPCredential{ProviderID: "google.com", RequestURI: uri.String(), SessionID: callback.SessionID}}, nil
}

func (p *GithubGCIPProvider) Start(ctx context.Context) (Authorization, error) {
	body := map[string]any{"providerId": "github.com", "continueUri": p.identity.RedirectURL, "oauthScope": "user:email"}

	raw, _ := json.Marshal(body)
	var result struct {
		AuthURI   string `json:"authUri"`
		SessionID string `json:"sessionId"`
	}
	if err := p.identity.request(ctx, p.identity.AuthURIURL, "application/json", bytes.NewReader(raw), &result, false); err != nil {
		return Authorization{}, err
	}
	uri, err := url.Parse(result.AuthURI)
	if err != nil || uri.Scheme != "https" || uri.Host != "github.com" || uri.User != nil || uri.Fragment != "" || result.SessionID == "" {
		return Authorization{}, ErrUpstream
	}
	if uri.Path != "/login/oauth/authorize" {
		return Authorization{}, ErrUpstream
	}
	query, err := url.ParseQuery(uri.RawQuery)
	if err != nil || len(query["state"]) != 1 || query.Get("state") == "" || len(query.Get("state")) > 4096 {
		return Authorization{}, ErrUpstream
	}
	return Authorization{URL: result.AuthURI, State: query.Get("state"), SessionID: result.SessionID}, nil
}

// Exchange prepares the authorization response; GCIP performs the token exchange.
func (p *GithubGCIPProvider) Exchange(_ context.Context, callback OAuthCallback) (Credential[GCIPCredential], error) {
	if callback.SessionID == "" || callback.State == "" || callback.Code == "" {
		return Credential[GCIPCredential]{}, ErrCredential
	}
	uri, err := url.Parse(p.identity.RedirectURL)
	if err != nil {
		return Credential[GCIPCredential]{}, ErrUpstream
	}
	query := uri.Query()
	query.Set("state", callback.State)
	query.Set("code", callback.Code)
	uri.RawQuery = query.Encode()
	return Credential[GCIPCredential]{Data: GCIPCredential{ProviderID: "github.com", RequestURI: uri.String(), SessionID: callback.SessionID}}, nil
}
