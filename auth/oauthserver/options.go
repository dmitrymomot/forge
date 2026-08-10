package oauthserver

import (
	"context"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// Option configures New.
type Option func(*serverConfig)

type serverConfig struct {
	clk           clock.Clock
	codeStore     cache.Store
	scope         func(ctx context.Context) (string, error)
	authenticator func(w http.ResponseWriter, r *http.Request) (string, bool)
	codeKeyset    *keyset.Keyset
	userClaims    func(ctx context.Context, subject string) (map[string]any, error)
	cfg           Config
	codeTTL       time.Duration
}

// WithConfig replaces the default Config (typically env-loaded). A zero
// TokenTTL falls back to the DefaultConfig value.
func WithConfig(cfg Config) Option {
	return func(c *serverConfig) { c.cfg = cfg }
}

// WithClock overrides the time source (tests).
func WithClock(clk clock.Clock) Option {
	return func(c *serverConfig) { c.clk = clk }
}

// WithScope sets the tenancy hook for the management methods: CreateClient
// stamps the returned value as TenantID; Get/Rotate/Revoke/List are
// filtered by it; hook errors fail closed. Token issuance is NOT
// ctx-scoped — the tenant claim comes from the client record.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(c *serverConfig) { c.scope = fn }
}

// WithAuthenticator sets the authorize-endpoint user seam: return the
// logged-in subject, or write your own response (e.g. redirect to the
// login page, returning here afterwards) and return ok=false.
func WithAuthenticator(fn func(w http.ResponseWriter, r *http.Request) (string, bool)) Option {
	return func(c *serverConfig) { c.authenticator = fn }
}

// WithCodeStore sets the TTL-KV store that makes authorization codes
// single-use (atomic SetNX claim on the code's jti). Memory store works
// for a single instance; a fleet needs a shared durable Store.
func WithCodeStore(cs cache.Store) Option {
	return func(c *serverConfig) { c.codeStore = cs }
}

// WithCodeKeyset sets the keyset sealing authorization codes
// (crypto/token; independent from the jwt.Signer's keys).
func WithCodeKeyset(ks *keyset.Keyset) Option {
	return func(c *serverConfig) { c.codeKeyset = ks }
}

// WithUserClaims enriches id_tokens with per-user claims (email, name,
// roles) so first-party apps skip a post-login lookup. Reserved claims
// (iss, sub, aud, exp, iat, nonce) cannot be overridden. Hook errors fail
// the token request.
func WithUserClaims(fn func(ctx context.Context, subject string) (map[string]any, error)) Option {
	return func(c *serverConfig) { c.userClaims = fn }
}

// WithCodeTTL bounds authorization-code lifetime. A non-positive duration
// falls back to the default (60s).
func WithCodeTTL(d time.Duration) Option {
	return func(c *serverConfig) { c.codeTTL = d }
}
