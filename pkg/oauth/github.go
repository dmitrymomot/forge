package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	githubOAuth "golang.org/x/oauth2/github"
)

const (
	// GitHubProviderName is the identifier for GitHub OAuth provider.
	GitHubProviderName = "github"
	githubUserURL      = "https://api.github.com/user"
	githubEmailsURL    = "https://api.github.com/user/emails"
)

// GitHubDefaultScopes returns the default scopes for GitHub OAuth.
func GitHubDefaultScopes() []string {
	return []string{"read:user", "user:email"}
}

// GitHubProvider implements Provider for GitHub OAuth.
type GitHubProvider struct {
	config     *oauth2.Config
	httpClient *http.Client
}

// NewGitHubProvider creates a new GitHub OAuth provider.
// Returns an error if ClientID or ClientSecret is empty.
func NewGitHubProvider(cfg GitHubConfig, opts ...Option) (*GitHubProvider, error) {
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
		scopes = GitHubDefaultScopes()
	}

	return &GitHubProvider{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint:     githubOAuth.Endpoint,
		},
		httpClient: o.httpClient,
	}, nil
}

// Name returns the provider identifier.
func (p *GitHubProvider) Name() string {
	return GitHubProviderName
}

// AuthCodeURL generates the authorization URL.
func (p *GitHubProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return p.config.AuthCodeURL(state, opts...)
}

// Exchange trades an authorization code for tokens.
func (p *GitHubProvider) Exchange(ctx context.Context, code, redirectURI string) (*oauth2.Token, error) {
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

// FetchUserInfo retrieves user information from GitHub.
//
// Email resolution prefers the primary verified email and deliberately falls
// back to any other verified email when no primary verified email exists.
// Returns ErrNoEmail if GitHub returns no emails at all, or ErrEmailNotVerified
// if emails exist but none are verified.
func (p *GitHubProvider) FetchUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	ctx = p.contextWithHTTPClient(ctx)
	client := p.config.Client(ctx, token)

	ghUser, err := p.fetchUser(client)
	if err != nil {
		return nil, err
	}

	email, err := p.fetchPrimaryVerifiedEmail(client)
	if err != nil {
		return nil, err
	}

	return &UserInfo{
		ID:      fmt.Sprintf("%d", ghUser.ID),
		Email:   email,
		Name:    ghUser.Name,
		Picture: ghUser.AvatarURL,
	}, nil
}

func (p *GitHubProvider) contextWithHTTPClient(ctx context.Context) context.Context {
	if p.httpClient != nil {
		return context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	}
	return ctx
}

func (p *GitHubProvider) fetchUser(client *http.Client) (*githubUser, error) {
	resp, err := client.Get(githubUserURL)
	if err != nil {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("fetch user: %w", err))
	}
	if resp == nil {
		return nil, errors.Join(ErrNilResponse, errors.New("unexpected nil response from github user endpoint"))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Join(ErrRequestFailed, fmt.Errorf("user request failed: status=%d", resp.StatusCode))
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, errors.Join(ErrDecodeFailed, fmt.Errorf("decode user: %w", err))
	}

	return &user, nil
}

func (p *GitHubProvider) fetchPrimaryVerifiedEmail(client *http.Client) (string, error) {
	resp, err := client.Get(githubEmailsURL)
	if err != nil {
		return "", errors.Join(ErrFetchFailed, fmt.Errorf("fetch emails: %w", err))
	}
	if resp == nil {
		return "", errors.Join(ErrNilResponse, errors.New("unexpected nil response from github emails endpoint"))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.Join(ErrRequestFailed, fmt.Errorf("emails request failed: status=%d", resp.StatusCode))
	}

	var emails []githubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", errors.Join(ErrDecodeFailed, fmt.Errorf("decode emails: %w", err))
	}

	// GitHub returns an empty list when the user:email scope was not granted
	// or the account has no email addresses. Distinguish this from the case
	// where emails exist but none are verified so callers can react
	// differently (e.g. request the user:email scope vs. ask the user to
	// verify their address).
	if len(emails) == 0 {
		return "", ErrNoEmail
	}

	// Prefer the primary verified email — this is the address GitHub treats as
	// the account's canonical email and is the correct identity to bind to.
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	// Deliberate fallback: if no primary verified email exists (e.g. the user
	// changed their primary to an as-yet-unverified address), accept any other
	// verified email rather than failing the login. This trades strictness for
	// usability and is safe because the address is still provider-verified.
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}

	return "", ErrEmailNotVerified
}

type githubUser struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	ID        int    `json:"id"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}
