package problem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// ErrNotProblem is returned by Decode when the response body is not a problem
// document. Match it with errors.Is.
var ErrNotProblem = errors.New("problem: not a problem+json response")

// maxProblemBytes caps the body Decode will read, guarding against a hostile or
// runaway upstream.
const maxProblemBytes = 1 << 20 // 1 MiB

// Error implements error with a single-line summary. The response body written by
// the responders is unaffected — this is for logs and errors.Is chains.
func (p *Problem) Error() string {
	if p.Code != "" {
		return fmt.Sprintf("problem: %d %s [%s]", p.Status, p.Title, p.Code)
	}
	return fmt.Sprintf("problem: %d %s", p.Status, p.Title)
}

// Is matches target by its non-zero fields: a *Problem target matches when
// (target.Status == 0 || target.Status == p.Status) &&
// (target.Code == "" || target.Code == p.Code). So errors.Is(err,
// &Problem{Code:"rate_limited"}) matches by code, &Problem{Status:429} by status,
// and &Problem{} any Problem. A non-*Problem target never matches.
func (p *Problem) Is(target error) bool {
	t, ok := target.(*Problem)
	if !ok {
		return false
	}
	if t.Status != 0 && t.Status != p.Status {
		return false
	}
	if t.Code != "" && t.Code != p.Code {
		return false
	}
	return true
}

// Decode reads an RFC 9457 problem+json response body into a *Problem. It caps the
// body at 1 MiB, fills Status from resp.StatusCode when the body omits it, and does
// NOT close resp.Body (the caller owns it). A body that is not a problem document
// returns ErrNotProblem.
func Decode(resp *http.Response) (*Problem, error) {
	if resp == nil || resp.Body == nil {
		return nil, ErrNotProblem
	}
	var p Problem
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxProblemBytes))
	if err := dec.Decode(&p); err != nil {
		return nil, ErrNotProblem
	}
	ct := resp.Header.Get("Content-Type")
	looksProblem := strings.Contains(ct, "application/problem+json") ||
		p.Status != 0 || p.Code != "" || p.Title != "" || p.Type != ""
	if !looksProblem {
		return nil, ErrNotProblem
	}
	if p.Status == 0 {
		p.Status = resp.StatusCode
	}
	return &p, nil
}
