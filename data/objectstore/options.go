package objectstore

import (
	"context"
	"fmt"
)

type config struct {
	scope   func(context.Context) (string, error)
	allowed map[string]struct{}
	optErrs []error
	maxSize int64
}

// Option configures New.
type Option func(*config)

// WithScope derives a tenant scope from context for every operation: keys
// are stored under "<scope>/" transparently, List is confined to the scope,
// and cross-tenant keys are unreachable. Fail-closed: a hook error, empty
// scope, or scope that breaks the key grammar fails the operation with
// ErrScope. A nil fn leaves the bucket unscoped — single-tenant apps pay
// nothing.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithMaxSize caps the byte size Put accepts; larger content aborts with
// ErrTooLarge before completing the backend write. Zero (the default) means
// no cap. Negative n is a configuration error.
func WithMaxSize(n int64) Option {
	return func(c *config) {
		if n < 0 {
			c.optErrs = append(c.optErrs, fmt.Errorf("objectstore: negative max size %d", n))
			return
		}
		c.maxSize = n
	}
}

// WithAllowedTypes restricts Put to content whose magic-byte-detected MIME
// is one of mimes (e.g. "image/png", "application/pdf"); anything else —
// including content filetype cannot recognize — is rejected with
// ErrUnsupportedType. Without this option any recognizable or unknown
// content is accepted and stored with its detected type (unknown content is
// stored as application/octet-stream). An empty MIME is a configuration
// error, and so is calling the option with no MIMEs at all (a slice that
// expanded to nothing must not silently mean "allow everything").
func WithAllowedTypes(mimes ...string) Option {
	return func(c *config) {
		if len(mimes) == 0 {
			c.optErrs = append(c.optErrs, fmt.Errorf("objectstore: WithAllowedTypes with no MIMEs"))
			return
		}
		if c.allowed == nil {
			c.allowed = make(map[string]struct{}, len(mimes))
		}
		for _, m := range mimes {
			if m == "" {
				c.optErrs = append(c.optErrs, fmt.Errorf("objectstore: empty MIME in allowed types"))
				return
			}
			c.allowed[m] = struct{}{}
		}
	}
}
