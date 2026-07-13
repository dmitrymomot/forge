package oauthclient

import "time"

// Identity is the normalized user identity produced by Complete/Exchange.
type Identity struct {
	// Raw holds the full id_token claims (OIDC path) or the provider profile
	// payload (Identity-hook path) for fields the normalized set omits.
	Raw           map[string]any
	Provider      string
	Subject       string
	Email         string
	Name          string
	Picture       string
	EmailVerified bool
}

// TokenResponse is the provider's raw token-endpoint response. It is exposed
// once in Result; storing it (for later provider-API calls) is the caller's
// concern — this package is login-only.
type TokenResponse struct {
	// ExpiresAt is Now+expires_in at exchange time; zero when the provider
	// omitted expires_in.
	ExpiresAt    time.Time
	AccessToken  string
	TokenType    string
	RefreshToken string
	IDToken      string
	Scope        string
}
