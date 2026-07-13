package oauthserver

import (
	"slices"
	"time"
)

// Grant types supported by the token endpoint.
const (
	GrantClientCredentials = "client_credentials"
	GrantAuthorizationCode = "authorization_code"
)

// Client is a registered OAuth client: an M2M partner
// (client_credentials), a first-party app/mirror (authorization_code), or
// both. Secrets are stored as SHA-256 digests; plaintext exists only in
// the CreateClient/RotateSecret return value.
type Client struct {
	RevokedAt time.Time
	CreatedAt time.Time
	ID        string
	Name      string
	// TenantID scopes the client to a tenant; empty in single-tenant apps.
	TenantID   string
	SecretHash []byte
	// Scopes is the allowlist; requests must be a subset.
	Scopes []string
	// Grants lists the grant types this client may use.
	Grants []string
	// RedirectURIs is the exact-match callback allowlist (authorization_code).
	RedirectURIs []string
	// TokenTTL overrides the server default when positive.
	TokenTTL time.Duration
}

// Revoked reports whether the client has been revoked.
func (c Client) Revoked() bool { return !c.RevokedAt.IsZero() }

// AllowsGrant reports whether g is in the client's grant allowlist.
func (c Client) AllowsGrant(g string) bool { return slices.Contains(c.Grants, g) }

// AllowsRedirect reports whether uri exactly matches a registered redirect URI.
func (c Client) AllowsRedirect(uri string) bool {
	return uri != "" && slices.Contains(c.RedirectURIs, uri)
}

// AllowsScopes reports whether every requested scope is in the allowlist.
func (c Client) AllowsScopes(requested []string) bool {
	for _, s := range requested {
		if !slices.Contains(c.Scopes, s) {
			return false
		}
	}
	return true
}

// clone deep-copies c so store internals never alias caller slices.
func (c Client) clone() Client {
	c.SecretHash = slices.Clone(c.SecretHash)
	c.Scopes = slices.Clone(c.Scopes)
	c.Grants = slices.Clone(c.Grants)
	c.RedirectURIs = slices.Clone(c.RedirectURIs)
	return c
}
