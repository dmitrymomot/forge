package ratelimit

import (
	"math"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/web/middleware"
)

// KeyFunc selects the limiter key for a request (e.g. client IP or user ID).
type KeyFunc func(*http.Request) string

type middlewareConfig struct {
	responder func(http.ResponseWriter, *http.Request, Result)
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithResponder overrides the 429 response (default plain text). Use it to emit
// problem+json via web/problem.
func WithResponder(fn func(http.ResponseWriter, *http.Request, Result)) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// Middleware limits by key, emitting RateLimit-* headers and a 429 (with
// Retry-After) when exceeded. On a Store error it fails open (serves the
// request) to avoid turning a limiter outage into an app outage.
func (l *Limiter) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware {
	cfg := middlewareConfig{responder: defaultResponder}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res, err := l.Allow(r.Context(), key(r))
			if err != nil {
				next.ServeHTTP(w, r) // fail open
				return
			}
			writeRateLimitHeaders(w, res)
			if !res.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
				cfg.responder(w, r, res)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeRateLimitHeaders(w http.ResponseWriter, res Result) {
	w.Header().Set("RateLimit-Limit", strconv.FormatInt(res.Limit, 10))
	w.Header().Set("RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
	reset := int(math.Ceil(res.RetryAfter.Seconds()))
	if res.Allowed {
		reset = 0
	}
	w.Header().Set("RateLimit-Reset", strconv.Itoa(reset))
}

func defaultResponder(w http.ResponseWriter, _ *http.Request, _ Result) {
	http.Error(w, ErrLimited.Error(), http.StatusTooManyRequests)
}
