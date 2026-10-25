package identity

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

type Google struct {
	Config    oauth2.Config
	Client    *http.Client
	Validator *idtoken.Validator
}

func NewGoogle(ctx context.Context, id, secret, redirect string, client *http.Client) (*Google, error) {
	validator, err := idtoken.NewValidator(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, ErrUpstream
	}
	return &Google{Config: oauth2.Config{ClientID: id, ClientSecret: secret, RedirectURL: redirect, Endpoint: google.Endpoint, Scopes: []string{"openid", "email", "profile"}}, Client: client, Validator: validator}, nil
}
func (g *Google) AuthorizationURL(state, verifier, nonce string) string {
	return g.Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("prompt", "select_account"))
}
func (g *Google) Exchange(ctx context.Context, code, verifier, nonce string) (Credential, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, g.Client)
	token, err := g.Config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Credential{}, transportError(err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return Credential{}, ErrCredential
	}
	payload, err := g.Validator.Validate(ctx, raw, g.Config.ClientID)
	if err != nil {
		return Credential{}, ErrCredential
	}
	if (payload.Issuer != "https://accounts.google.com" && payload.Issuer != "accounts.google.com") || payload.Subject == "" || payload.Expires <= time.Now().Unix() || payload.Claims["nonce"] != nonce {
		return Credential{}, ErrCredential
	}
	if azp, ok := payload.Claims["azp"]; ok && azp != g.Config.ClientID {
		return Credential{}, ErrCredential
	}
	return Credential{ProviderID: "google.com", IDToken: raw}, nil
}
