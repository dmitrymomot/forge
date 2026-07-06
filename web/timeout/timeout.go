package timeout

import (
	"context"
	"errors"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// New returns middleware that puts a deadline on every request context and
// answers 504 when a ctx-respecting handler returns without writing after
// the deadline expired. Enforcement is cooperative — handlers that ignore
// their context keep running; the deadline reaches them via r.Context().
//
// Do not wrap streaming routes (SSE, long-poll): compose with
// middleware.Skip to exempt them.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{
		cfg:       DefaultConfig(),
		responder: problem.JSON(problem.WithStatus(http.StatusGatewayTimeout)),
	}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	d := cf.cfg.Timeout
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			rw := middleware.WrapWriter(w)
			next.ServeHTTP(rw, r.WithContext(ctx))
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && !rw.Wrote() {
				cf.responder(rw, r, ErrTimeout)
			}
		})
	}, nil
}
