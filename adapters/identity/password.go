package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

type EmailPassword struct {
	Client            *http.Client
	APIKey, SignInURL string
}

func NewEmailPassword(client *http.Client, key string) *EmailPassword {
	return &EmailPassword{Client: client, APIKey: key, SignInURL: "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword"}
}

func (p *EmailPassword) SignIn(ctx context.Context, credential Credential[PasswordCredential]) (Tokens, error) {
	body, _ := json.Marshal(map[string]any{"email": credential.Data.Email, "password": credential.Data.Password, "returnSecureToken": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.SignInURL+"?key="+url.QueryEscape(p.APIKey), bytes.NewReader(body))
	if err != nil {
		return Tokens{}, ErrUpstream
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.Client.Do(req)
	if err != nil {
		return Tokens{}, transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var result struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		// Only recognize documented credential failures; never expose upstream text.
		if resp.StatusCode == http.StatusBadRequest && json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) == nil {
			switch result.Error.Message {
			case "INVALID_LOGIN_CREDENTIALS", "EMAIL_NOT_FOUND", "INVALID_PASSWORD", "USER_DISABLED", "INVALID_EMAIL":
				return Tokens{}, ErrCredential
			}
		}
		return Tokens{}, ErrUpstream
	}
	var result struct {
		Email        string `json:"email"`
		LocalID      string `json:"localId"`
		IDToken      string `json:"idToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    string `json:"expiresIn"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.LocalID == "" {
		return Tokens{}, ErrUpstream
	}
	tokens, err := checkedTokens(result.IDToken, result.RefreshToken, result.ExpiresIn)
	if err != nil {
		return Tokens{}, err
	}
	tokens.LocalID = result.LocalID
	tokens.Email = result.Email
	return tokens, nil
}

func (p *EmailPassword) Refresh(context.Context, string) (Tokens, error) {
	return Tokens{}, ErrNotImplemented
}
