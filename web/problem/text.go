package problem

import (
	"bytes"
	"net/http"
	"text/template"

	"github.com/dmitrymomot/forge/core/bufpool"
	"github.com/dmitrymomot/forge/ui/render"
)

// defaultTextBody renders a Problem as plain text (no HTML). Trailing newlines
// keep the body readable in a terminal/browser.
const defaultTextBody = "{{.Status}} {{.Title}}\n" +
	"{{with .Detail}}\n{{.}}\n{{end}}" +
	"{{with .Code}}code: {{.}}\n{{end}}" +
	"{{range $f, $m := .Fields}}{{$f}}: {{$m}}\n{{end}}"

var defaultTextTemplate = template.Must(template.New("problem").Parse(defaultTextBody))

// Text returns a Responder that writes err as text/plain, rendered with a
// text/template (never html/template). WithTemplate overrides the layout.
func Text(opts ...Option) Responder {
	c := newConfig(opts...)
	tmpl := c.template
	if tmpl == nil {
		tmpl = defaultTextTemplate
	}
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p := c.build(err, r)
		c.log5xx(r, p, err)
		var body string
		_ = bufpool.Do(func(buf *bytes.Buffer) error {
			if e := tmpl.Execute(buf, p); e != nil {
				// A broken custom template must not yield an empty body: fall back to
				// the default layout so the status still ships with a readable message.
				buf.Reset()
				if e2 := defaultTextTemplate.Execute(buf, p); e2 != nil {
					return e2
				}
			}
			body = buf.String()
			return nil
		})
		_ = render.Text(w, p.Status, body)
	}
}
