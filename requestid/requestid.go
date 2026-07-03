// Package requestid attaches a correlation ID to each request: an accepted
// inbound header value or a freshly generated ULID.
package requestid

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/ctxkey"
	"github.com/dmitrymomot/forge/id"
	"github.com/dmitrymomot/forge/logger"
	"github.com/dmitrymomot/forge/middleware"
)

var idKey = ctxkey.New[string]("requestid")

func defaultGenerator() string { return id.NewULID().String() }

type config struct {
	generator    func() string
	header       string
	trustInbound bool
}

// Option configures the requestid middleware.
type Option func(*config)

// WithHeader sets the request/response header name (default "X-Request-ID").
func WithHeader(name string) Option {
	return func(c *config) {
		if name != "" {
			c.header = name
		}
	}
}

// WithGenerator sets the ID generator (default a ULID string).
func WithGenerator(gen func() string) Option {
	return func(c *config) {
		if gen != nil {
			c.generator = gen
		}
	}
}

// WithTrustInbound controls whether a valid inbound header is accepted (default true).
func WithTrustInbound(trust bool) Option { return func(c *config) { c.trustInbound = trust } }

// New returns middleware that stores the request ID in context, echoes it on the
// response header, and exposes it via From and LogExtractor.
func New(opts ...Option) middleware.Middleware {
	c := config{header: "X-Request-ID", generator: defaultGenerator, trustInbound: true}
	for _, o := range opts {
		o(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := ""
			if c.trustInbound {
				if v := r.Header.Get(c.header); validInbound(v) {
					rid = v
				}
			}
			if rid == "" {
				rid = c.generator()
			}
			w.Header().Set(c.header, rid)
			next.ServeHTTP(w, r.WithContext(idKey.With(r.Context(), rid)))
		})
	}
}

// validInbound accepts a non-empty, printable-ASCII value of at most 128 bytes,
// so a client cannot inject control characters into logs or the echoed header.
func validInbound(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// From returns the request ID stored by New.
func From(ctx context.Context) (string, bool) { return idKey.From(ctx) }

// LogExtractor adds a "request_id" attribute when one is present.
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	rid, ok := idKey.From(ctx)
	if !ok || rid == "" {
		return slog.Attr{}, false
	}
	return slog.String("request_id", rid), true
}
