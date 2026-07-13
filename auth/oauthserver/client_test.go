package oauthserver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func TestClientHelpers(t *testing.T) {
	c := oauthserver.Client{
		Grants:       []string{oauthserver.GrantClientCredentials},
		Scopes:       []string{"read:odds", "write:bets"},
		RedirectURIs: []string{"https://m1.example.com/cb"},
	}
	assert.True(t, c.AllowsGrant("client_credentials"))
	assert.False(t, c.AllowsGrant("authorization_code"))
	assert.True(t, c.AllowsRedirect("https://m1.example.com/cb"))
	assert.False(t, c.AllowsRedirect("https://m1.example.com/cb/"), "exact match only")
	assert.True(t, c.AllowsScopes([]string{"read:odds"}))
	assert.True(t, c.AllowsScopes(nil), "empty request is always a subset")
	assert.False(t, c.AllowsScopes([]string{"read:odds", "admin"}))
	assert.False(t, c.Revoked())
}
