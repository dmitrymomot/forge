package oauthserver

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/crypto/digest"
)

// secretPrefix makes leaked client secrets recognizable to scanners.
const secretPrefix = "osk_"

// CreateClientInput describes a new registry client.
type CreateClientInput struct {
	Name         string
	Scopes       []string
	Grants       []string
	RedirectURIs []string
	// TokenTTL overrides the server default when positive.
	TokenTTL time.Duration
}

// ClientCredentials carries the plaintext secret — returned exactly once
// by CreateClient/RotateSecret, never stored.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

var knownGrants = []string{GrantClientCredentials, GrantAuthorizationCode}

// CreateClient registers a client and returns its credentials. The secret
// is shown once; only its SHA-256 digest is stored.
func (s *Server) CreateClient(ctx context.Context, in CreateClientInput) (*ClientCredentials, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name required", ErrInvalidInput)
	}
	if len(in.Grants) == 0 {
		return nil, fmt.Errorf("%w: at least one grant required", ErrInvalidInput)
	}
	for _, g := range in.Grants {
		if !slices.Contains(knownGrants, g) {
			return nil, fmt.Errorf("%w: unknown grant %q", ErrInvalidInput, g)
		}
	}
	if slices.Contains(in.Grants, GrantAuthorizationCode) {
		if len(in.RedirectURIs) == 0 {
			return nil, fmt.Errorf("%w: authorization_code requires redirect URIs", ErrInvalidInput)
		}
		for _, u := range in.RedirectURIs {
			parsed, err := url.Parse(u)
			if err != nil || !parsed.IsAbs() || parsed.Fragment != "" {
				return nil, fmt.Errorf("%w: redirect URI %q must be an absolute URL without fragment", ErrInvalidInput, u)
			}
		}
	}
	tenant, err := s.scopeTenant(ctx)
	if err != nil {
		return nil, err
	}
	secret := secretPrefix + random.URLSafe(32)
	cl := Client{
		ID:           s.idgen.New(),
		Name:         in.Name,
		SecretHash:   digest.SHA256([]byte(secret)),
		Scopes:       in.Scopes,
		Grants:       in.Grants,
		RedirectURIs: in.RedirectURIs,
		TenantID:     tenant,
		TokenTTL:     in.TokenTTL,
		CreatedAt:    s.clk.Now().UTC(),
	}
	if err := s.store.Create(ctx, cl); err != nil {
		return nil, err
	}
	return &ClientCredentials{ClientID: cl.ID, ClientSecret: secret}, nil
}

// RotateSecret replaces the client's secret, returning the new plaintext
// once. Tokens already issued stay valid until their exp.
func (s *Server) RotateSecret(ctx context.Context, clientID string) (*ClientCredentials, error) {
	cl, err := s.getScoped(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if cl.Revoked() {
		return nil, ErrClientRevoked
	}
	secret := secretPrefix + random.URLSafe(32)
	cl.SecretHash = digest.SHA256([]byte(secret))
	if err := s.store.Update(ctx, cl); err != nil {
		return nil, err
	}
	return &ClientCredentials{ClientID: cl.ID, ClientSecret: secret}, nil
}

// RevokeClient disables the client. New tokens stop immediately;
// outstanding JWTs remain valid until their exp (≤ the token TTL) — there
// is no introspection endpoint by design.
func (s *Server) RevokeClient(ctx context.Context, clientID string) error {
	cl, err := s.getScoped(ctx, clientID)
	if err != nil {
		return err
	}
	if cl.Revoked() {
		return nil
	}
	cl.RevokedAt = s.clk.Now().UTC()
	return s.store.Update(ctx, cl)
}

// GetClient returns one client (tenancy-scoped when WithScope is set).
func (s *Server) GetClient(ctx context.Context, clientID string) (Client, error) {
	return s.getScoped(ctx, clientID)
}

// ListClients returns the tenant's clients (all clients without WithScope).
func (s *Server) ListClients(ctx context.Context) ([]Client, error) {
	t, err := s.scopeTenant(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.List(ctx, t)
}

// getScoped fetches a client and enforces the tenancy scope; a
// cross-tenant hit reads as not-found so existence never leaks.
func (s *Server) getScoped(ctx context.Context, clientID string) (Client, error) {
	cl, err := s.store.Get(ctx, clientID)
	if err != nil {
		return Client{}, err
	}
	if s.scope != nil {
		t, err := s.scopeTenant(ctx)
		if err != nil {
			return Client{}, err
		}
		if cl.TenantID != t {
			return Client{}, ErrClientNotFound
		}
	}
	return cl, nil
}

// scopeTenant resolves the tenancy scope for the management methods. With no
// scope hook it returns ("", nil) — single-tenant, every client is the
// caller's. With a hook configured it must yield a non-empty tenant; an empty
// string is rejected fail-closed, because Store.List treats "" as the
// "every tenant" sentinel and an empty tenant would otherwise leak all
// clients across tenants.
func (s *Server) scopeTenant(ctx context.Context) (string, error) {
	if s.scope == nil {
		return "", nil
	}
	t, err := s.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("oauthserver: scope hook: %w", err)
	}
	if t == "" {
		return "", fmt.Errorf("%w: scope hook returned an empty tenant", ErrInvalidConfig)
	}
	return t, nil
}
