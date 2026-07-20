package metrics

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
)

type middlewareConfig struct {
	skip       func(*http.Request) bool
	pathFunc   func(*http.Request) string
	buckets    []float64
	labelNames []string
	labelFuncs []func(context.Context) string
}

// MiddlewareOption configures NewMiddleware.
type MiddlewareOption func(*middlewareConfig)

// WithSkip skips instrumentation for requests where pred returns true
// (e.g. health checks). A nil pred is ignored.
func WithSkip(pred func(*http.Request) bool) MiddlewareOption {
	return func(c *middlewareConfig) {
		if pred != nil {
			c.skip = pred
		}
	}
}

// WithDurationBuckets overrides the duration histogram buckets
// (default DefaultBuckets, in seconds). An empty slice is ignored.
func WithDurationBuckets(buckets []float64) MiddlewareOption {
	return func(c *middlewareConfig) {
		if len(buckets) > 0 {
			c.buckets = slices.Clone(buckets)
		}
	}
}

// WithPathFunc overrides how the path label is derived. The default uses
// r.Pattern — the matched ServeMux route with the method prefix stripped
// ("/users/{id}", never the raw URL path, which would explode label
// cardinality) — and "unmatched" when no pattern matched. Routers that don't
// populate r.Pattern (chi, gorilla) need this hook to expose their route
// template. A nil fn is ignored.
func WithPathFunc(fn func(*http.Request) string) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.pathFunc = fn
		}
	}
}

// WithContextLabel adds a label whose value is derived from the request
// context — the tenancy seam: pair it with a tenant-from-context lookup to
// scope request metrics per tenant. fn sees the context as this middleware
// received it, so mount the metrics middleware INSIDE whatever middleware
// populates the value (e.g. tenant resolution). Fail-closed: when fn returns
// "" the value is recorded as "unknown", never attributed elsewhere. Keep
// value cardinality bounded (label sets are aggregated per distinct value).
// Panics on an empty name or nil fn: a misconfigured scope hook must not
// silently drop scoping.
func WithContextLabel(name string, fn func(context.Context) string) MiddlewareOption {
	if name == "" || fn == nil {
		panic("metrics: WithContextLabel requires a name and a non-nil fn")
	}
	return func(c *middlewareConfig) {
		c.labelNames = append(c.labelNames, name)
		c.labelFuncs = append(c.labelFuncs, fn)
	}
}

// NewMiddleware returns middleware recording three instruments per request:
// http_requests_total (counter), http_request_duration_seconds (histogram),
// and http_requests_in_flight (gauge), the first two labeled by method, path,
// status, and any WithContextLabel additions. Place it OUTSIDE the recoverer
// so panics are still recorded (as the 500 the recoverer writes). Panics on a
// nil Recorder.
func NewMiddleware(rec Recorder, opts ...MiddlewareOption) middleware.Middleware {
	if rec == nil {
		panic("metrics: NewMiddleware requires a Recorder")
	}
	c := middlewareConfig{pathFunc: patternPath}
	for _, o := range opts {
		o(&c)
	}
	names := append([]string{"method", "path", "status"}, c.labelNames...)
	requests := rec.Counter("http_requests_total", "Total HTTP requests processed.", names...)
	duration := rec.Histogram("http_request_duration_seconds", "HTTP request duration in seconds.", c.buckets, names...)
	inflight := rec.Gauge("http_requests_in_flight", "HTTP requests currently being served.")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.skip != nil && c.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			inflight.Add(1)
			defer inflight.Add(-1)
			rw := middleware.WrapWriter(w)
			start := time.Now()
			next.ServeHTTP(rw, r)
			elapsed := time.Since(start)

			status := rw.Status()
			if status == 0 {
				status = http.StatusOK
			}
			values := make([]string, 0, len(names))
			values = append(values, normalizeMethod(r.Method), c.pathFunc(r), strconv.Itoa(status))
			for _, fn := range c.labelFuncs {
				v := fn(r.Context())
				if v == "" {
					v = "unknown"
				}
				values = append(values, v)
			}
			requests.Inc(values...)
			duration.Observe(elapsed.Seconds(), values...)
		})
	}
}

// normalizeMethod caps method-label cardinality: anything outside the nine
// standard HTTP methods (arbitrary strings are valid on the wire) is "OTHER".
func normalizeMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return m
	}
	return "OTHER"
}

func patternPath(r *http.Request) string {
	p := r.Pattern
	if i := strings.IndexByte(p, ' '); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		return "unmatched"
	}
	return p
}
