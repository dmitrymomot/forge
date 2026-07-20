package tracing

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/web/middleware"
)

// W3C Trace Context header names, in canonical MIME form so they can be used
// as direct http.Header map keys.
const (
	TraceparentHeader = "Traceparent"
	TracestateHeader  = "Tracestate"
)

// maxTracestate caps the combined inbound tracestate forwarded downstream (the
// spec recommends vendors keep the list under 512 characters). An oversized
// value is dropped whole rather than truncated mid-member.
const maxTracestate = 512

type middlewareConfig struct {
	skip         func(*http.Request) bool
	pathFunc     func(*http.Request) string
	attrNames    []string
	attrFuncs    []func(context.Context) string
	trustInbound bool
}

// MiddlewareOption configures NewMiddleware.
type MiddlewareOption func(*middlewareConfig)

// WithSkip skips tracing for requests where pred returns true (e.g. health
// checks). A nil pred is ignored.
func WithSkip(pred func(*http.Request) bool) MiddlewareOption {
	return func(c *middlewareConfig) {
		if pred != nil {
			c.skip = pred
		}
	}
}

// WithPathFunc overrides how the route is derived for the span name and
// http.route attribute. The default uses r.Pattern — the matched ServeMux
// route with the method prefix stripped ("/users/{id}", never the raw URL
// path) — and "" when no pattern matched, which leaves the span named after
// the method alone. Routers that don't populate r.Pattern (chi, gorilla) need
// this hook to expose their route template. A nil fn is ignored.
func WithPathFunc(fn func(*http.Request) string) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.pathFunc = fn
		}
	}
}

// WithTrustInbound controls whether inbound traceparent/tracestate headers are
// honored (default true). Disable on public edges where an arbitrary client
// must not steer trace ids or downstream sampling decisions.
func WithTrustInbound(trust bool) MiddlewareOption {
	return func(c *middlewareConfig) { c.trustInbound = trust }
}

// WithContextAttr adds a span attribute whose value is derived from the
// request context — the tenancy seam: pair it with a tenant-from-context
// lookup to scope request spans per tenant. fn sees the context as this
// middleware received it, so mount the tracing middleware INSIDE whatever
// middleware populates the value. Fail-closed: when fn returns "" the value is
// recorded as "unknown", never attributed elsewhere. Panics on an empty key or
// nil fn: a misconfigured scope hook must not silently drop scoping.
func WithContextAttr(key string, fn func(context.Context) string) MiddlewareOption {
	if key == "" || fn == nil {
		panic("tracing: WithContextAttr requires a key and a non-nil fn")
	}
	return func(c *middlewareConfig) {
		c.attrNames = append(c.attrNames, key)
		c.attrFuncs = append(c.attrFuncs, fn)
	}
}

// NewMiddleware returns middleware that parses the inbound W3C traceparent
// into a remote parent, starts a KindServer span around the handler, and ends
// it when the handler returns. The span starts named after the method and is
// renamed "GET /users/{id}" once the matched route is known; it carries
// http.request.method, url.path, http.route, http.response.status_code, and
// any WithContextAttr additions, with 5xx responses marked StatusError. Place
// it OUTSIDE the recoverer so panicking requests still close their span (as
// the 500 the recoverer writes). Panics on a nil Tracer.
func NewMiddleware(tr Tracer, opts ...MiddlewareOption) middleware.Middleware {
	if tr == nil {
		panic("tracing: NewMiddleware requires a Tracer")
	}
	c := middlewareConfig{pathFunc: patternPath, trustInbound: true}
	for _, o := range opts {
		o(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.skip != nil && c.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			if c.trustInbound {
				if sc, err := ParseTraceparent(r.Header.Get(TraceparentHeader)); err == nil {
					if ts := strings.Join(r.Header.Values(TracestateHeader), ","); ts != "" && len(ts) <= maxTracestate {
						sc.TraceState = ts
					}
					ctx = ContextWithRemote(ctx, sc)
				}
			}
			method := normalizeMethod(r.Method)
			startAttrs := make([]slog.Attr, 0, 3)
			startAttrs = append(startAttrs,
				slog.String("http.request.method", method),
				slog.String("url.path", r.URL.Path),
			)
			if method != r.Method {
				// Cardinality control folded the verb to "OTHER"; keep the raw
				// method debuggable per OTel semconv.
				startAttrs = append(startAttrs, slog.String("http.request.method_original", r.Method))
			}
			ctx, span := tr.Start(ctx, method, WithKind(KindServer), WithAttributes(startAttrs...))
			defer span.End()

			rw := middleware.WrapWriter(w)
			// Serve via the context-carrying copy and read the matched pattern
			// off that same copy: ServeMux sets Pattern on the request it
			// receives, not on this middleware's original.
			r = r.WithContext(ctx)
			next.ServeHTTP(rw, r)

			if route := c.pathFunc(r); route != "" {
				span.SetName(method + " " + route)
				span.SetAttributes(slog.String("http.route", route))
			}
			status := rw.Status()
			if status == 0 {
				status = http.StatusOK
			}
			attrs := make([]slog.Attr, 0, 1+len(c.attrFuncs))
			attrs = append(attrs, slog.Int("http.response.status_code", status))
			for i, fn := range c.attrFuncs {
				v := fn(ctx)
				if v == "" {
					v = "unknown"
				}
				attrs = append(attrs, slog.String(c.attrNames[i], v))
			}
			span.SetAttributes(attrs...)
			if status >= 500 {
				span.SetStatus(StatusError, http.StatusText(status))
			}
		})
	}
}

// normalizeMethod caps span-name cardinality: anything outside the nine
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
	return p
}
