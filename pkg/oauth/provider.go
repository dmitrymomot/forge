package oauth

import (
	"context"

	"golang.org/x/oauth2"
)

// UserInfo represents provider-agnostic user information
// retrieved from an OAuth provider's userinfo endpoint.
type UserInfo struct {
	ID      string // Provider's unique user identifier
	Email   string
	Name    string
	Picture string
}

// Provider abstracts provider-specific OAuth operations.
// Each provider (Google, GitHub, etc.) implements this interface.
// Provider implementations handle all provider-specific details internally,
// including email verification checks.
type Provider interface {
	// Name returns the provider identifier (e.g., "google", "github").
	Name() string

	// AuthCodeURL generates the authorization URL for the OAuth flow.
	AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string

	// Exchange trades an authorization code for tokens.
	Exchange(ctx context.Context, code, redirectURI string) (*oauth2.Token, error)

	// FetchUserInfo retrieves user information using the access token.
	// Implementations must verify the user's email and return ErrEmailNotVerified
	// if the email is not verified.
	FetchUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error)
}
