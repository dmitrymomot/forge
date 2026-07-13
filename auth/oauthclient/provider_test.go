package oauthclient_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func TestGooglePreset(t *testing.T) {
	p := oauthclient.Google(oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "sec"})
	assert.Equal(t, "cid", p.ClientID)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth", p.AuthURL)
	assert.Equal(t, "https://oauth2.googleapis.com/token", p.TokenURL)
	assert.Equal(t, "https://www.googleapis.com/oauth2/v3/certs", p.JWKSURL)
	assert.Equal(t, "https://accounts.google.com", p.Issuer)
	assert.Equal(t, []string{"openid", "email", "profile"}, p.Scopes)
	assert.Nil(t, p.Identity, "google is OIDC — no identity hook")
}

func TestGooglePresetScopeOverride(t *testing.T) {
	p := oauthclient.Google(oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "s", Scopes: []string{"openid"}})
	assert.Equal(t, []string{"openid"}, p.Scopes)
}

func TestGitHubPreset(t *testing.T) {
	p := oauthclient.GitHub(oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "sec"})
	assert.Equal(t, "https://github.com/login/oauth/authorize", p.AuthURL)
	assert.Equal(t, "https://github.com/login/oauth/access_token", p.TokenURL)
	assert.Equal(t, []string{"read:user", "user:email"}, p.Scopes)
	assert.NotNil(t, p.Identity, "github is not OIDC — identity hook required")
	assert.Empty(t, p.Issuer)
}

func TestProviderErrorMessage(t *testing.T) {
	e := &oauthclient.ProviderError{Code: "access_denied", Description: "user said no"}
	assert.Equal(t, "oauthclient: provider error: access_denied: user said no", e.Error())
	e2 := &oauthclient.ProviderError{Code: "access_denied"}
	assert.Equal(t, "oauthclient: provider error: access_denied", e2.Error())
}
