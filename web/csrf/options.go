package csrf

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	responder  problem.Responder
	skip       func(*http.Request) bool
	cookieName string
	header     string
	formField  string
}

// Option configures the middleware.
type Option func(*config)

// WithCookieName overrides the token cookie name (default "__Host-csrf",
// falling back to "csrf" when the codec policy can't satisfy __Host-).
func WithCookieName(name string) Option { return func(c *config) { c.cookieName = name } }

// WithHeader overrides the token header name (default "X-CSRF-Token").
func WithHeader(name string) Option { return func(c *config) { c.header = name } }

// WithFormField overrides the form field name (default "csrf_token").
func WithFormField(name string) Option { return func(c *config) { c.formField = name } }

// WithResponder overrides the rejection response (default problem.JSON 403).
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

// WithSkip exempts requests matching pred (e.g. webhook endpoints verified
// by signature instead).
func WithSkip(pred func(*http.Request) bool) Option { return func(c *config) { c.skip = pred } }
