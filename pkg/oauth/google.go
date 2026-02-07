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
	googleUserInfoURL  = "https://www.googleapis.com/oauth2/v2/userinfo"
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
		body, _ := io.ReadAll(resp.Body)
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

// googleUserInfo represents the response from Google's userinfo endpoint.
type googleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	VerifiedEmail bool   `json:"verified_email"`
}
