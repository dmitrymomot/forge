package problem

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"text/template"

	"github.com/dmitrymomot/forge/core/errorsx"
	"github.com/dmitrymomot/forge/core/validate"
	"github.com/dmitrymomot/forge/web/request"
)

// Problem is an RFC 9457 problem document with forge extensions (Code, Fields).
type Problem struct {
	Fields   map[string]string `json:"fields,omitempty"`
	Type     string            `json:"type,omitempty"`
	Title    string            `json:"title,omitempty"`
	Detail   string            `json:"detail,omitempty"`
	Instance string            `json:"instance,omitempty"`
	Code     string            `json:"code,omitempty"`
	Status   int               `json:"status"`
}

// Responder writes err as an HTTP error response. It is the seam every
// error-writing middleware/handler accepts.
type Responder func(w http.ResponseWriter, r *http.Request, err error)

type config struct {
	statusOf    func(error) int
	logger      *slog.Logger
	template    *template.Template // nil => Text uses the package default
	typeBaseURI string
	forceStatus int
}

// Option configures From and the predefined responders.
type Option func(*config)

// WithLogger makes a responder log 5xx errors (with the request context, so
// request_id/client_ip ride along). The body still never contains the error text.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// WithStatusOf overrides the error->status resolver (default request.StatusCode).
func WithStatusOf(fn func(error) int) Option {
	return func(c *config) {
		if fn != nil {
			c.statusOf = fn
		}
	}
}

// WithStatus forces a specific status regardless of the error.
func WithStatus(code int) Option { return func(c *config) { c.forceStatus = code } }

// WithTypeBaseURI sets a base URI prepended to the error Code to form Problem.Type.
func WithTypeBaseURI(uri string) Option { return func(c *config) { c.typeBaseURI = uri } }

// WithTemplate overrides the text/template the Text responder renders (default
// defaultTextTemplate). It has no effect on the JSON responder.
func WithTemplate(t *template.Template) Option {
	return func(c *config) {
		if t != nil {
			c.template = t
		}
	}
}

func newConfig(opts ...Option) config {
	c := config{statusOf: request.StatusCode}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (c config) status(err error) int {
	if c.forceStatus != 0 {
		return c.forceStatus
	}
	return c.statusOf(err)
}

// build assembles the Problem. r may be nil (From without a request).
func (c config) build(err error, r *http.Request) Problem {
	status := c.status(err)
	p := Problem{
		Status: status,
		Title:  http.StatusText(status),
		Type:   "about:blank",
	}
	if code, ok := errorsx.Code(err); ok {
		p.Code = code
		if c.typeBaseURI != "" {
			p.Type = c.typeBaseURI + code
		}
	}
	if fields := extractFields(err); len(fields) > 0 {
		p.Fields = fields
	}
	if status < 500 && err != nil {
		p.Detail = err.Error()
	}
	if r != nil {
		p.Instance = r.URL.Path
	}
	return p
}

// log5xx logs a 5xx error (with the request context, so request_id/client_ip ride
// along) when a logger is configured. The error text never enters the response.
// All responders call it so the "log but never leak on 5xx" rule stays DRY.
func (c config) log5xx(r *http.Request, p Problem, err error) {
	if p.Status < 500 || c.logger == nil {
		return
	}
	c.logger.LogAttrs(r.Context(), slog.LevelError, "request error",
		slog.Int("status", p.Status),
		slog.String("error", err.Error()),
	)
}

// From maps err to a Problem document without writing a response.
func From(err error, opts ...Option) Problem {
	return newConfig(opts...).build(err, nil)
}

// extractFields pulls per-field messages from a validate.Errors or *request.Error.
func extractFields(err error) map[string]string {
	if ve, ok := errors.AsType[validate.Errors](err); ok {
		out := make(map[string]string, len(ve))
		for field, vs := range ve.ByField() {
			parts := make([]string, len(vs))
			for i, v := range vs {
				parts[i] = v.String()
			}
			out[field] = strings.Join(parts, "; ")
		}
		return out
	}
	if re, ok := errors.AsType[*request.Error](err); ok {
		key := string(re.Source)
		if re.Key != "" {
			key = re.Key
		}
		return map[string]string{key: re.Kind.String()}
	}
	return nil
}
