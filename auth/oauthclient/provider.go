package oauthclient

import (
	"context"
	"fmt"
	"net/http"
)

// Provider describes one OAuth2/OIDC provider. Google/GitHub presets and
// Discover fill it; hand-building one is the recipe for odd providers and
// for forge's own oauthserver.
type Provider struct {
	// AuthParams are extra authorize-URL parameters (prompt, hd, allow_signup…).
	// Protocol-owned parameters are reserved; a collision fails AuthURL/Begin.
	AuthParams map[string]string
	// Identity fetches the user identity for providers without OIDC
	// id_tokens (GitHub). When nil the OIDC id_token path is used and
	// Issuer + JWKSURL are required.
	Identity     func(ctx context.Context, hc *http.Client, token TokenResponse) (Identity, error)
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	JWKSURL      string
	Issuer       string
	// RedirectURL overrides the client-wide default for this provider.
	RedirectURL string
	Scopes      []string
}

// ProviderConfig is the env-loadable per-provider config shared by all
// presets and Discover. Nest it with a tagged prefix to separate providers:
//
//	type Config struct {
//	    Google oauthclient.ProviderConfig `env:"OAUTH_GOOGLE"`
//	    GitHub oauthclient.ProviderConfig `env:"OAUTH_GITHUB"`
//	}
type ProviderConfig struct {
	AuthParams   map[string]string
	ClientID     string   `env:"CLIENT_ID,required"`
	ClientSecret string   `env:"CLIENT_SECRET,required"`
	RedirectURL  string   `env:"REDIRECT_URL"`
	Scopes       []string `env:"SCOPES"`
}

// reservedParams are authorize-URL parameters owned by the flow; AuthParams
// may not override them.
//
//nolint:unused // consumed by Client.AuthURL/Begin, added in a later task of this bundle
var reservedParams = map[string]bool{
	"client_id": true, "redirect_uri": true, "response_type": true,
	"scope": true, "state": true, "nonce": true,
	"code_challenge": true, "code_challenge_method": true,
}

//nolint:unused // consumed by Client.AuthURL/Begin, added in a later task of this bundle
func (p Provider) validate() error {
	if p.ClientID == "" || p.AuthURL == "" || p.TokenURL == "" {
		return fmt.Errorf("%w: provider needs ClientID, AuthURL and TokenURL", ErrInvalidConfig)
	}
	if p.Identity == nil && (p.Issuer == "" || p.JWKSURL == "") {
		return fmt.Errorf("%w: OIDC provider needs Issuer and JWKSURL (or set an Identity hook)", ErrInvalidConfig)
	}
	for k := range p.AuthParams {
		if reservedParams[k] {
			return fmt.Errorf("%w: %q", ErrReservedParam, k)
		}
	}
	return nil
}

// Google returns the Google OIDC preset. Default scopes: openid email profile.
func Google(cfg ProviderConfig) Provider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	return Provider{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		JWKSURL:      "https://www.googleapis.com/oauth2/v3/certs",
		Issuer:       "https://accounts.google.com",
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		AuthParams:   cfg.AuthParams,
	}
}
