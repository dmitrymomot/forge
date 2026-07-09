package idempotency

import "time"

type config struct {
	methods       map[string]bool
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
			set[x] = true
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
func WithProcessingTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.processingTTL = d
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
