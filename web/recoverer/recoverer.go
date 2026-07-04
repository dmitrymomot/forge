package recoverer

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	responder problem.Responder
	logger    *slog.Logger
}

// Option configures the recoverer middleware.
type Option func(*config)

// WithResponder sets the error responder used to write the 500 (default:
// problem.JSON() forced to HTTP 500). NOTE: recoverer forces 500 only on the
// DEFAULT responder. A custom responder is used as-is, so you MUST force HTTP 500
// on it yourself (e.g. problem.WithStatus(http.StatusInternalServerError)) — a
// panic reaches the responder as an ErrPanic-wrapped plain error, which otherwise
// resolves to 400 and leaks the panic text in the body.
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

// WithLogger sets the logger for panic reports (default slog.Default()).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// New returns middleware that recovers panics, logs them at Error with the
// request context, and (if nothing was written yet) writes a 500 via the
// responder. http.ErrAbortHandler is re-panicked so net/http can abort silently.
func New(opts ...Option) middleware.Middleware {
	c := config{responder: problem.JSON(problem.WithStatus(http.StatusInternalServerError)), logger: slog.Default()}
	for _, o := range opts {
		o(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := middleware.WrapWriter(w)
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				c.logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)
				if !rw.Wrote() {
					c.responder(rw, r, fmt.Errorf("%w: %v", ErrPanic, v))
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}
