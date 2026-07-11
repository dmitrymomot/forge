package loadshed

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
)

type middlewareConfig struct {
	responder func(http.ResponseWriter, *http.Request)
	skip      func(*http.Request) bool
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithResponder overrides the shed response (default 503 + Retry-After).
func WithResponder(fn func(http.ResponseWriter, *http.Request)) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// WithSkip never-sheds requests for which fn returns true (health, admin).
func WithSkip(fn func(*http.Request) bool) MiddlewareOption {
	return func(c *middlewareConfig) { c.skip = fn }
}

// Middleware sheds a slice of traffic under overload, returning 503 via the
// responder; admitted requests are served and their Ticket released.
func (s *Shedder) Middleware(opts ...MiddlewareOption) middleware.Middleware {
	cfg := middlewareConfig{responder: defaultResponder}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skip != nil && cfg.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			tk, ok := s.Acquire(r.Context())
			if !ok {
				cfg.responder(w, r)
				return
			}
			defer tk.Release()
			next.ServeHTTP(w, r)
		})
	}
}

func defaultResponder(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "service overloaded", http.StatusServiceUnavailable)
}
