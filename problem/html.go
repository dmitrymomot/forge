package problem

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/middleware"
	"github.com/dmitrymomot/forge/render"
)

// HTML returns a Responder that renders the Problem with a caller-supplied
// html/template. name selects a {{define}} block ("" runs t.Execute). The markup
// lives entirely in the consumer's template — forge ships none. WithLogger logs 5xx.
func HTML(t *template.Template, name string, opts ...Option) Responder {
	c := newConfig(opts...)
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p := c.build(err, r)
		c.log5xx(r, p, err)
		rw := middleware.WrapWriter(w)
		if rerr := render.HTML(rw, p.Status, t, name, p); rerr != nil {
			c.renderFallback(r, rw, p, rerr)
		}
	}
}

// Component returns a Responder that renders the Problem as a render.Component
// (e.g. a templ component) built by build. render.Templ sets text/html.
func Component(build func(Problem) render.Component, opts ...Option) Responder {
	c := newConfig(opts...)
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p := c.build(err, r)
		c.log5xx(r, p, err)
		rw := middleware.WrapWriter(w)
		if rerr := render.Templ(r.Context(), rw, p.Status, build(p)); rerr != nil {
			c.renderFallback(r, rw, p, rerr)
		}
	}
}

// renderFallback handles a failed error-page render. render.HTML and render.Templ
// are transactional — on a nil/broken template or an execution error they return
// before committing the header — so when nothing was written we still emit the
// status and a minimal text/plain body (forge ships no markup, so there is no
// default HTML page to fall back to). The render error is logged when a logger is
// configured, so a broken template is diagnosable rather than a silent empty body.
func (c config) renderFallback(r *http.Request, rw middleware.ResponseWriter, p Problem, rerr error) {
	if c.logger != nil {
		c.logger.LogAttrs(r.Context(), slog.LevelError, "problem: error page render failed",
			slog.Int("status", p.Status),
			slog.String("error", rerr.Error()),
		)
	}
	if rw.Wrote() {
		return
	}
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(p.Status)
	_, _ = io.WriteString(rw, p.Title)
}
