package cookie

import (
	"net/http"
	"time"
)

type policy struct {
	path     string
	domain   string
	maxAge   time.Duration
	sameSite http.SameSite
	secure   bool
	httpOnly bool
}

// Option configures the codec-wide policy.
type Option func(*policy)

// WithPath sets the default cookie path (default "/").
func WithPath(p string) Option { return func(c *policy) { c.path = p } }

// WithDomain sets the cookie domain (default host-only).
func WithDomain(d string) Option { return func(c *policy) { c.domain = d } }

// WithMaxAge sets the default lifetime; 0 means session cookie.
func WithMaxAge(d time.Duration) Option { return func(c *policy) { c.maxAge = d } }

// WithSameSite sets the default SameSite mode (default Lax).
func WithSameSite(s http.SameSite) Option { return func(c *policy) { c.sameSite = s } }

// WithSecure toggles the Secure flag (default true).
func WithSecure(v bool) Option { return func(c *policy) { c.secure = v } }

// WithHTTPOnly toggles the HttpOnly flag (default true).
func WithHTTPOnly(v bool) Option { return func(c *policy) { c.httpOnly = v } }

// WriteOption overrides the policy for a single Set call.
type WriteOption func(*policy)

// WithWriteMaxAge overrides the lifetime for this write; negative expires now.
func WithWriteMaxAge(d time.Duration) WriteOption { return func(c *policy) { c.maxAge = d } }

// WithWritePath overrides the path for this write.
func WithWritePath(p string) WriteOption { return func(c *policy) { c.path = p } }

// WithWriteDomain overrides the domain for this write.
func WithWriteDomain(d string) WriteOption { return func(c *policy) { c.domain = d } }

// WithWriteSameSite overrides the SameSite mode for this write.
func WithWriteSameSite(s http.SameSite) WriteOption { return func(c *policy) { c.sameSite = s } }
