package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	googleOAuth "golang.org/x/oauth2/google"
)

const (
	// GoogleProviderName is the identifier for Google OAuth provider.
	GoogleProviderName = "google"
	// googleUserInfoURL is Google's current OpenID Connect userinfo endpoint.
	// It supersedes the deprecated oauth2/v2/userinfo endpoint. The response
	// uses OIDC standard claim names ("sub", "email_verified") rather than the
	// v2 names ("id", "verified_email"); see googleUserInfo for the mapping.
	googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

	// maxErrorBodyBytes bounds how much of a non-OK response body is read into
	// memory for error reporting, preventing an unbounded allocation on a
	// hostile or misbehaving endpoint.
	maxErrorBodyBytes = 4 << 10 // 4 KiB
)

// GoogleDefaultScopes returns the default scopes for Google OAuth.
func GoogleDefaultScopes() []string {
	return []string{
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
}

// GoogleProvider implements Provider for Google OAuth.
type GoogleProvider struct {
	config     *oauth2.Config
	httpClient *http.Client
}

// NewGoogleProvider creates a new Google OAuth provider.
// Returns an error if ClientID or ClientSecret is empty.
func NewGoogleProvider(cfg GoogleConfig, opts ...Option) (*GoogleProvider, error) {
	if cfg.ClientID == "" {
		return nil, ErrMissingClientID
	}
	if cfg.ClientSecret == "" {
		return nil, ErrMissingClientSecret
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = GoogleDefaultScopes()
	}

	return &GoogleProvider{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint:     googleOAuth.Endpoint,
		},
		httpClient: o.httpClient,
	}, nil
}

// Name returns the provider identifier.
func (p *GoogleProvider) Name() string {
	return GoogleProviderName
}

// AuthCodeURL generates the authorization URL.
func (p *GoogleProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return p.config.AuthCodeURL(state, opts...)
}

// Exchange trades an authorization code for tokens.
func (p *GoogleProvider) Exchange(ctx context.Context, code, redirectURI string) (*oauth2.Token, error) {
	cfg := p.config
	if redirectURI != "" {
		cfg = &oauth2.Config{
			ClientID:     p.config.ClientID,
			ClientSecret: p.config.ClientSecret,
			RedirectURL:  redirectURI,
			Scopes:       p.config.Scopes,
			Endpoint:     p.config.Endpoint,
		}
	}
	ctx = p.contextWithHTTPClient(ctx)
	return cfg.Exchange(ctx, code)
}

// FetchUserInfo retrieves user information from Google.
// Returns ErrEmailNotVerified if the user's email is not verified.
func (p *GoogleProvider) FetchUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	ctx = p.contextWithHTTPClient(ctx)
	client := p.config.Client(ctx, token)

	resp, err := client.Get(googleUserInfoURL)
	if err != nil {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("fetch userinfo: %w", err))
	}
	if resp == nil {
		return nil, errors.Join(ErrNilResponse, errors.New("unexpected nil response from google userinfo endpoint"))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, errors.Join(ErrRequestFailed, fmt.Errorf("userinfo request failed: status=%d body=%s", resp.StatusCode, body))
	}

	var googleUser googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&googleUser); err != nil {
		return nil, errors.Join(ErrDecodeFailed, fmt.Errorf("decode userinfo: %w", err))
	}

	if !googleUser.VerifiedEmail {
		return nil, ErrEmailNotVerified
	}

	return &UserInfo{
		ID:      googleUser.ID,
		Email:   googleUser.Email,
		Name:    googleUser.Name,
		Picture: googleUser.Picture,
	}, nil
}

func (p *GoogleProvider) contextWithHTTPClient(ctx context.Context) context.Context {
	if p.httpClient != nil {
		return context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	}
	return ctx
}

// googleUserInfo represents the response from Google's OpenID Connect
// userinfo endpoint. The endpoint returns OIDC standard claims: the stable
// user identifier is "sub" (not the v2 "id") and verification status is
// "email_verified" (not the v2 "verified_email").
type googleUserInfo struct {
	ID            string `json:"sub"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	VerifiedEmail bool   `json:"email_verified"`
}
