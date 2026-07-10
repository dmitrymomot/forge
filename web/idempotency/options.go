package idempotency

import (
	"net/http"
	"strings"
	"time"
)

type config struct {
	methods       map[string]bool
	namespace     func(*http.Request) string
	header        string
	ttl           time.Duration
	processingTTL time.Duration
	maxBody       int64
	requireKey    bool
}

// Option configures New.
type Option func(*config)

// WithHeader overrides the idempotency key header (default "Idempotency-Key").
func WithHeader(name string) Option {
	return func(c *config) {
		if name != "" {
			c.header = name
		}
	}
}

// WithMethods overrides the guarded HTTP methods (default POST, PUT, PATCH, DELETE).
func WithMethods(m ...string) Option {
	return func(c *config) {
		set := make(map[string]bool, len(m))
		for _, x := range m {
			set[strings.ToUpper(x)] = true
		}
		c.methods = set
	}
}

// WithTTL sets how long a stored response stays replayable (default 24h).
func WithTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// WithProcessingTTL sets the lifetime of the in-flight claim marker (default 1m).
// A crashed first request's key auto-releases after this window.
//
// Set it above your worst-case handler latency: if the first request is still
// running when the marker expires, a concurrent retry can claim the key and
// execute the handler a second time.
func WithProcessingTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.processingTTL = d
		}
	}
}

// WithNamespace scopes the idempotency key by a per-request namespace — typically
// the authenticated tenant or user ID — so keys from different principals never
// collide in a shared Store. The namespace is prefixed to the store key; the
// header value and request fingerprint are unchanged.
// The namespace is length-framed into the store key, so it may safely contain any characters.
func WithNamespace(fn func(*http.Request) string) Option {
	return func(c *config) {
		if fn != nil {
			c.namespace = fn
		}
	}
}

// WithMaxBodySize caps the request body read for fingerprinting and the response
// body buffered for storage (default 1 MiB). Oversize requests get 413; oversize
// responses are sent to the client but not cached.
func WithMaxBodySize(n int64) Option {
	return func(c *config) {
		if n > 0 {
			c.maxBody = n
		}
	}
}

// WithRequireKey makes a guarded request without the key header fail with 400
// instead of passing through unguarded.
func WithRequireKey() Option { return func(c *config) { c.requireKey = true } }
