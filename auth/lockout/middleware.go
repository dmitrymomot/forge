package lockout

import (
	"context"
	"math"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/middleware"
)

// KeyFunc selects the lockout key for a request — e.g. a form email
// (r.PostFormValue parses and caches the form, so the handler's own
// PostFormValue calls still work) or a client IP. An empty key skips the
// check entirely; the handler's own validation rejects malformed logins.
// JSON-body extraction (read + restore r.Body) is the app's responsibility.
type KeyFunc func(*http.Request) string

var requestKey = ctxkey.New[string]("lockout")

// KeyFromContext returns the lockout key the middleware extracted for this
// request, so handlers call Fail/Reset with the identical key.
func KeyFromContext(ctx context.Context) (string, bool) {
	return requestKey.From(ctx)
}

type middlewareConfig struct {
	responder func(http.ResponseWriter, *http.Request, Result)
	failOpen  bool
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithResponder overrides the 429 response (default plain text). Use it to
// emit problem+json via web/problem. Retry-After is already set when it runs.
func WithResponder(fn func(http.ResponseWriter, *http.Request, Result)) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// WithFailOpen serves requests when the lockout check errors instead of
// returning 503. The default fails closed: brute-force protection must not
// silently disable during a store outage.
func WithFailOpen() MiddlewareOption {
	return func(c *middlewareConfig) { c.failOpen = true }
}

// Middleware gates requests on the lockout state of key(r): locked requests
// get 429 with Retry-After; unlocked requests proceed with the extracted key
// stashed in the context (KeyFromContext). It covers only the Allow half —
// handlers still call Fail/Reset with the attempt's outcome.
func (l *Locker) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware {
	cfg := middlewareConfig{responder: defaultResponder}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			res, err := l.Allow(r.Context(), k)
			if err != nil {
				if cfg.failOpen {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			if res.Locked {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
				cfg.responder(w, r, res)
				return
			}
			next.ServeHTTP(w, r.WithContext(requestKey.With(r.Context(), k)))
		})
	}
}

func defaultResponder(w http.ResponseWriter, _ *http.Request, _ Result) {
	http.Error(w, ErrLocked.Error(), http.StatusTooManyRequests)
}
