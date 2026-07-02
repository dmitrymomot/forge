package problem

import (
	"html/template"
	"net/http"

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
		_ = render.HTML(w, p.Status, t, name, p)
	}
}

// Component returns a Responder that renders the Problem as a render.Component
// (e.g. a templ component) built by build. render.Templ sets text/html.
func Component(build func(Problem) render.Component, opts ...Option) Responder {
	c := newConfig(opts...)
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p := c.build(err, r)
		c.log5xx(r, p, err)
		_ = render.Templ(r.Context(), w, p.Status, build(p))
	}
}
