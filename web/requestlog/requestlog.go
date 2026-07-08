package requestlog

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
)

type config struct {
	levelFunc func(status int) slog.Level
	skip      func(*http.Request) bool
}

// Option configures the requestlog middleware.
type Option func(*config)

// WithLevelFunc maps the response status to a log level (default 5xx->Error,
// 4xx->Warn, else Info).
func WithLevelFunc(fn func(status int) slog.Level) Option {
	return func(c *config) {
		if fn != nil {
			c.levelFunc = fn
		}
	}
}

// WithSkip skips logging for requests where pred returns true (e.g. health checks).
func WithSkip(pred func(*http.Request) bool) Option { return func(c *config) { c.skip = pred } }

func defaultLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// New returns middleware that logs method, path, status, duration, and bytes for
// each request, using the request context so wired ContextExtractors (request_id,
// client_ip) are included automatically. A nil log uses slog.Default().
func New(log *slog.Logger, opts ...Option) middleware.Middleware {
	if log == nil {
		log = slog.Default()
	}
	c := config{levelFunc: defaultLevel}
	for _, o := range opts {
		o(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.skip != nil && c.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			rw := middleware.WrapWriter(w)
			start := time.Now()
			next.ServeHTTP(rw, r)
			status := rw.Status()
			if status == 0 {
				status = http.StatusOK
			}
			log.LogAttrs(r.Context(), c.levelFunc(status), "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Duration("duration", time.Since(start)),
				slog.Int64("bytes", rw.Written()),
			)
		})
	}
}
