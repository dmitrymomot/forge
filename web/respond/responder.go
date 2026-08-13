package respond

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// Responder writes the answers of one router tree in that tree's dialect. Build one
// per tree in the composition root: the page tree renders refusals as HTML, the API
// tree as application/problem+json, and the same handler mounted in both needs no
// change and reads no Accept header.
type Responder struct {
	onProblem problem.Responder
	logger    *slog.Logger
}

// ResponderOption configures a Responder.
type ResponderOption func(*Responder)

// WithProblem sets the responder that renders every refusal, which is what makes the
// tree's dialect a wiring decision. The default is problem.JSON().
func WithProblem(p problem.Responder) ResponderOption {
	return func(rs *Responder) {
		if p != nil {
			rs.onProblem = p
		}
	}
}

// WithLogger sets the logger that records a response which failed after the status
// was already sent. The default discards.
func WithLogger(l *slog.Logger) ResponderOption {
	return func(rs *Responder) {
		if l != nil {
			rs.logger = l
		}
	}
}

// New builds a Responder. With no options it answers refusals as problem+json and
// logs nothing.
func New(opts ...ResponderOption) *Responder {
	rs := &Responder{onProblem: problem.JSON(), logger: logger.NewNope()}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

// Wrap turns a Handler into an http.Handler: it calls h, runs the response's
// WithBefore side effects, then writes the response. An error at any of those steps
// reaches Fail instead, so nothing is half-written.
//
// A Respond that fails is split by whether the status was already committed. The
// transactional writers (JSON, HTML, Templ) encode into a buffer first and write
// nothing on failure, so that error still reaches Fail and the client gets a real
// status instead of a bare 200 with an empty body. A streaming writer that failed
// mid-body has already committed, so the error is only logged.
func (rs *Responder) Wrap(h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, err := h(r)
		if err != nil {
			rs.Fail(w, r, err)
			return
		}
		if response == nil {
			rs.Fail(w, r, ErrNoResponse)
			return
		}
		rw := middleware.WrapWriter(w)
		if runner, ok := response.(beforeRunner); ok {
			if err := runner.runBefore(rw); err != nil {
				rs.Fail(rw, r, fmt.Errorf("respond: side effect failed: %w", err))
				return
			}
		}
		if err := response.Respond(rw, r); err != nil {
			if !rw.Wrote() {
				rs.Fail(rw, r, fmt.Errorf("respond: writing the response failed: %w", err))
				return
			}
			rs.logger.ErrorContext(r.Context(), "response failed after it started",
				slog.String("path", r.URL.Path),
				slog.String("error", err.Error()))
		}
	})
}

// WrapFunc is Wrap for a plain func with the Handler signature.
func (rs *Responder) WrapFunc(h func(r *http.Request) (Response, error)) http.Handler {
	return rs.Wrap(h)
}

// NotFound is the handler a router mounts for a path it does not know. It answers in
// the dialect of this tree, not in whatever the request asked for.
func (rs *Responder) NotFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.Fail(w, r, ErrNotFound)
	})
}

// MethodNotAllowed is the handler a router mounts for a path it knows under another
// method.
func (rs *Responder) MethodNotAllowed() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.Fail(w, r, ErrMethodNotAllowed)
	})
}

// Fail writes the answer err deserves, through this tree's problem responder. It is
// exported so middleware that owns its own status (a deadline, a refused token) can
// answer in the same dialect as the handlers it wraps.
func (rs *Responder) Fail(w http.ResponseWriter, r *http.Request, err error) {
	rs.onProblem(w, r, err)
}

// StatusOf maps this package's sentinels to their status and reports 0 for anything
// else, which is the shape problem.WithStatusOf accepts as a first pass:
//
//	problem.JSON(problem.WithStatusOf(func(err error) int {
//		if code := respond.StatusOf(err); code != 0 {
//			return code
//		}
//		return request.StatusCode(err)
//	}))
func StatusOf(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrMethodNotAllowed):
		return http.StatusMethodNotAllowed
	case errors.Is(err, ErrNoResponse), errors.Is(err, ErrNoWriter):
		return http.StatusInternalServerError
	default:
		return 0
	}
}
