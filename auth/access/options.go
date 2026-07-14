package access

import (
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	resource  func(*http.Request) Resource
	subject   func(*http.Request) (Subject, bool)
	forbidden func(http.ResponseWriter, *http.Request)
	responder problem.Responder
	loadError func(http.ResponseWriter, *http.Request, error)
	logger    *slog.Logger
	explain   bool
}

// Option configures the access middleware and Model handlers.
type Option func(*config)

// WithResource sets how RequirePermission builds the Resource from the request
// (default: the zero Resource — a type-level check). Resolvers stay I/O-free.
// Not valid for Model.Handle (which derives the Resource from its Describe
// func); passing it there panics at startup rather than silently no-op'ing.
func WithResource(fn func(r *http.Request) Resource) Option {
	return func(c *config) {
		if fn != nil {
			c.resource = fn
		}
	}
}

// WithSubject overrides how the Subject is obtained (default: guard.From →
// SubjectFromIdentity). ok=false means fail-closed 403 with no decider call.
func WithSubject(fn func(r *http.Request) (Subject, bool)) Option {
	return func(c *config) {
		if fn != nil {
			c.subject = fn
		}
	}
}

// WithForbidden overrides the deny response with a custom renderer — an HTML
// 403 page or a redirect for server-rendered apps (default: problem.JSON 403).
// The denied Decision is on the request context (DecisionFrom) for the reason.
// Takes precedence over WithResponder on the deny path.
func WithForbidden(fn func(w http.ResponseWriter, r *http.Request)) Option {
	return func(c *config) {
		if fn != nil {
			c.forbidden = fn
		}
	}
}

// WithResponder overrides the 403 response (default problem.JSON 403).
func WithResponder(p problem.Responder) Option {
	return func(c *config) {
		if p != nil {
			c.responder = p
		}
	}
}

// WithLoadError overrides the Model load-failure response (default
// problem.JSON 404 with a generic body that cloaks both resource existence
// and the underlying error text; use this to surface the real error).
func WithLoadError(fn func(w http.ResponseWriter, r *http.Request, err error)) Option {
	return func(c *config) {
		if fn != nil {
			c.loadError = fn
		}
	}
}

// WithLogger logs decider errors at Warn (the client still gets a generic 403).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithExplain enables Decision.Trace accumulation for requests through this
// mount (debugging / an "explain" endpoint).
func WithExplain() Option {
	return func(c *config) { c.explain = true }
}

var (
	forbiddenResponder = problem.JSON(problem.WithStatus(http.StatusForbidden))
	notFoundResponder  = problem.JSON(problem.WithStatus(http.StatusNotFound))
)

func defaultSubject(r *http.Request) (Subject, bool) {
	id, ok := guard.From(r.Context())
	if !ok {
		return Subject{}, false
	}
	return SubjectFromIdentity(id), true
}

// defaultLoadError discards the raw Load error and responds with the generic
// errNotFound sentinel, so a raw DB/internal error never reaches the client.
func defaultLoadError(w http.ResponseWriter, r *http.Request, _ error) {
	notFoundResponder(w, r, errNotFound)
}

func newConfig(opts ...Option) config {
	c := config{
		subject:   defaultSubject,
		responder: forbiddenResponder,
		loadError: defaultLoadError,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (c config) reject(w http.ResponseWriter, r *http.Request, cause error) {
	if cause != nil && c.logger != nil {
		c.logger.WarnContext(r.Context(), "access decider error", slog.Any("error", cause))
	}
	if c.forbidden != nil {
		c.forbidden(w, r)
		return
	}
	c.responder(w, r, errDenied)
}
