package clientip

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/ctxkey"
	"github.com/dmitrymomot/forge/logger"
	"github.com/dmitrymomot/forge/middleware"
)

var ipKey = ctxkey.New[string]("clientip")

// Middleware resolves the client IP once per request (using opts) and caches it
// in the request context for Get, From, and LogExtractor.
func Middleware(opts ...Option) middleware.Middleware {
	c := newConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := c.resolve(r)
			next.ServeHTTP(w, r.WithContext(ipKey.With(r.Context(), ip)))
		})
	}
}

// From returns the cached client IP. ok reports whether Middleware ran — true
// even when the resolved IP is "" (resolution ran but nothing parsed).
func From(ctx context.Context) (string, bool) { return ipKey.From(ctx) }

// Get returns the client IP: the value cached by Middleware if it ran, else a
// safe RemoteAddr-only fallback. Handlers and other middleware should call Get
// rather than re-parsing headers.
func Get(r *http.Request) string {
	if ip, ok := From(r.Context()); ok {
		return ip
	}
	return Resolve(r)
}

// LogExtractor adds a "client_ip" attribute when Middleware cached a non-empty IP.
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	ip, ok := ipKey.From(ctx)
	if !ok || ip == "" {
		return slog.Attr{}, false
	}
	return slog.String("client_ip", ip), true
}
