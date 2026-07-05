package httpclient

import (
	"context"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
	"github.com/dmitrymomot/forge/resilience/retry"
)

type config struct {
	base         http.RoundTripper
	retryMethods map[string]bool
	headers      http.Header
	userAgent    string
	retryOpts    []retry.Option
	before       []func(*http.Request)
	after        []func(*http.Request, *http.Response)
	ctxHeaders   []func(context.Context) http.Header
	breakerOpts  []circuitbreaker.GroupOption
	timeout      time.Duration
	perAttempt   time.Duration
	useBreaker   bool
}

func newConfig(opts ...Option) config {
	c := config{
		base:         http.DefaultTransport,
		retryMethods: map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodPut: true, http.MethodDelete: true, http.MethodOptions: true},
		headers:      http.Header{},
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Option configures the client.
type Option func(*config)

// WithTimeout sets the overall http.Client timeout.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithPerAttemptTimeout bounds each individual attempt via context.
func WithPerAttemptTimeout(d time.Duration) Option { return func(c *config) { c.perAttempt = d } }

// WithRetry tunes the retry policy (default 3 attempts, jittered backoff).
func WithRetry(opts ...retry.Option) Option {
	return func(c *config) { c.retryOpts = append(c.retryOpts, opts...) }
}

// WithRetryMethods sets which HTTP methods are retried (default idempotent: GET,HEAD,PUT,DELETE,OPTIONS).
func WithRetryMethods(methods ...string) Option {
	return func(c *config) {
		c.retryMethods = map[string]bool{}
		for _, m := range methods {
			c.retryMethods[m] = true
		}
	}
}

// WithBaseTransport sets the innermost RoundTripper (default http.DefaultTransport).
func WithBaseTransport(rt http.RoundTripper) Option {
	return func(c *config) {
		if rt != nil {
			c.base = rt
		}
	}
}

// WithBefore runs fn on each outbound request before it is sent.
func WithBefore(fn func(*http.Request)) Option {
	return func(c *config) { c.before = append(c.before, fn) }
}

// WithAfter runs fn after each response is received.
func WithAfter(fn func(*http.Request, *http.Response)) Option {
	return func(c *config) { c.after = append(c.after, fn) }
}

// WithContextHeaders copies headers derived from the request context onto the
// outbound request (e.g. X-Request-ID, traceparent).
func WithContextHeaders(fn func(context.Context) http.Header) Option {
	return func(c *config) { c.ctxHeaders = append(c.ctxHeaders, fn) }
}

// WithHeader sets a static header on every request.
func WithHeader(key, value string) Option { return func(c *config) { c.headers.Set(key, value) } }

// WithUserAgent sets the User-Agent header on every request.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }

// WithBreakerGroup enables a per-host circuit breaker (off by default). Each
// attempt runs inside a circuitbreaker.Group keyed by request host; the breaker's
// open error carries Retry-After, so retry and breaker cooperate.
func WithBreakerGroup(opts ...circuitbreaker.GroupOption) Option {
	return func(c *config) {
		c.useBreaker = true
		c.breakerOpts = append(c.breakerOpts, opts...)
	}
}
