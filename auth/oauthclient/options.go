package oauthclient

import (
	"context"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// Option configures New.
type Option func(*clientConfig)

type clientConfig struct {
	providers   map[string]Provider
	source      func(ctx context.Context, name string) (Provider, error)
	binding     func(ctx context.Context) (string, error)
	hc          *http.Client
	clk         clock.Clock
	redirectURL string
	cookieName  string
	flowTTL     time.Duration
}

// WithProvider registers a static provider under name.
func WithProvider(name string, p Provider) Option {
	return func(c *clientConfig) { c.providers[name] = p }
}

// WithProviderSource sets the dynamic provider lookup consulted when a name
// is not in the static registry — the multi-tenant seam (resolve the tenant
// from ctx inside fn). Errors propagate: resolution fails closed.
func WithProviderSource(fn func(ctx context.Context, name string) (Provider, error)) Option {
	return func(c *clientConfig) { c.source = fn }
}

// WithScope sets the tenancy binding hook. Its value is sealed into the
// flow at Begin/AuthURL and must match at Complete/Exchange
// (ErrScopeBinding otherwise); hook errors fail closed.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(c *clientConfig) { c.binding = fn }
}

// WithHTTPClient sets the HTTP client used for token exchange, identity
// hooks, and JWKS fetches. Default: httpclient.New with a 15s timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) { c.hc = hc }
}

// WithRedirectURL sets the client-wide default redirect URL; a provider's
// RedirectURL overrides it.
func WithRedirectURL(u string) Option {
	return func(c *clientConfig) { c.redirectURL = u }
}

// WithCookieName sets the flow cookie name. Default "oauth_flow".
func WithCookieName(n string) Option {
	return func(c *clientConfig) { c.cookieName = n }
}

// WithFlowTTL bounds how long a started flow stays completable. A
// non-positive duration falls back to the default (10m).
func WithFlowTTL(d time.Duration) Option {
	return func(c *clientConfig) { c.flowTTL = d }
}

// WithClock overrides the time source (tests).
func WithClock(clk clock.Clock) Option {
	return func(c *clientConfig) { c.clk = clk }
}
