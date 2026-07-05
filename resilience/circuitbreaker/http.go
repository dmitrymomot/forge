package circuitbreaker

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// KeyFunc selects the breaker key for a request in GroupMiddleware. Returning
// an empty string skips the breaker for that request.
type KeyFunc func(*http.Request) string

// KeyByHost keys a Group breaker by request Host — the common reverse-proxy
// case, so GroupMiddleware needs no custom key code. Any func(*http.Request)
// string works for other strategies.
//
// Each distinct Host creates a breaker in the Group, reclaimed only after the
// Group's idle TTL. Behind a trusted proxy that normalizes Host to a bounded
// set this is fine; on an untrusted edge, arbitrary Host headers grow the key
// space until they idle out, so front it with an allowlist or a bounded key
// func there.
func KeyByHost(r *http.Request) string { return r.Host }

// OpenResponder writes the response when the circuit is open. retryAfter is the
// suggested delay before retrying (0 if unknown).
type OpenResponder func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration)

type middlewareConfig struct {
	isFailure func(status int) bool
	onOpen    OpenResponder
}

// MiddlewareOption configures Middleware, GroupMiddleware, and GroupKey.
type MiddlewareOption func(*middlewareConfig)

// WithFailurePredicate classifies a downstream response status as a breaker
// failure. Default: status >= 500. A nil predicate is ignored.
func WithFailurePredicate(fn func(status int) bool) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.isFailure = fn
		}
	}
}

// WithOpenResponder overrides the response written when the circuit is open.
// The default writes 503 with a Retry-After header and a plain-text body. A nil
// responder is ignored.
func WithOpenResponder(fn OpenResponder) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.onOpen = fn
		}
	}
}

func newMiddlewareConfig(opts ...MiddlewareOption) middlewareConfig {
	c := middlewareConfig{
		isFailure: func(status int) bool { return status >= 500 },
		onOpen:    defaultOpenResponder,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Middleware guards next with b. When the circuit is open it responds via the
// open responder (503 + Retry-After by default) without calling next. When the
// circuit is closed it calls next; a response whose status the failure
// predicate matches is recorded as a breaker failure, while the response itself
// still reaches the client unchanged. The returned value is assignable to
// web/middleware.Middleware.
func Middleware(b *Breaker, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serve(b, cfg, next, w, r)
		})
	}
}

// GroupMiddleware guards next with a per-request breaker chosen by key from g.
// A request whose key is empty bypasses the breaker. Otherwise identical to
// Middleware.
func GroupMiddleware(g *Group, key KeyFunc, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			serve(g.breaker(k), cfg, next, w, r)
		})
	}
}

// GroupKey guards next with the breaker named key from g — a fixed key chosen
// at wrap time, so a specific route handler gets its own breaker within one
// managed Group (shared options, unified eviction and introspection). An empty
// key bypasses the breaker.
func GroupKey(g *Group, key string, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			serve(g.breaker(key), cfg, next, w, r)
		})
	}
}

// errDownstreamFailure signals to the breaker that the guarded handler produced
// a failure status. It never leaves the middleware.
var errDownstreamFailure = errors.New("circuitbreaker: downstream failure")

func serve(b *Breaker, cfg middlewareConfig, next http.Handler, w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w}
	err := b.Do(r.Context(), func(ctx context.Context) error {
		next.ServeHTTP(rec, r.WithContext(ctx))
		if cfg.isFailure(rec.status()) {
			return errDownstreamFailure
		}
		return nil
	})
	if err == nil || errors.Is(err, errDownstreamFailure) {
		return // downstream already wrote its response (success or a matched failure)
	}
	if oe, ok := errors.AsType[*openError](err); ok {
		cfg.onOpen(w, r, oe.retryAfter)
	}
}

func defaultOpenResponder(w http.ResponseWriter, _ *http.Request, retryAfter time.Duration) {
	if secs := retryAfterSeconds(retryAfter); secs > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("service unavailable: circuit open\n"))
}

// retryAfterSeconds rounds a positive delay up to whole seconds (Retry-After is
// second-granular). Returns 0 for a non-positive delay so the header is omitted.
func retryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	secs := int(d / time.Second)
	if d%time.Second != 0 {
		secs++
	}
	return secs
}

// statusRecorder captures the response status for failure classification while
// passing writes through unchanged. It exposes the underlying writer via Unwrap
// so http.ResponseController reaches optional interfaces (Flusher for
// SSE/streaming, Hijacker, SetWriteDeadline, …) without falsely advertising
// them when the underlying writer lacks them.
type statusRecorder struct {
	http.ResponseWriter
	code  int
	wrote bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.code = code
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// status returns the committed status, or 200 when the handler wrote a body or
// nothing without an explicit WriteHeader (net/http's implicit 200).
func (r *statusRecorder) status() int {
	if !r.wrote {
		return http.StatusOK
	}
	return r.code
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
