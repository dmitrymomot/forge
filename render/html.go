package render

import (
	"fmt"
	"html/template"
	"net/http"
)

// HTML executes an html/template into a pooled buffer, then writes the result with
// the given status code. When name is "" it runs t.Execute(data); otherwise it runs
// t.ExecuteTemplate(name, data) — the layout / {{define}} pattern. It is
// transactional: a template execution error returns with nothing written to w. It
// returns ErrNilTemplate if t is nil (before writing anything). The Content-Type
// defaults to "text/html; charset=utf-8" unless the caller has already set one.
func HTML(w http.ResponseWriter, status int, t *template.Template, name string, data any) error {
	if t == nil {
		return ErrNilTemplate
	}
	buf := getBuf()
	defer putBuf(buf)
	var err error
	if name == "" {
		err = t.Execute(buf, data)
	} else {
		err = t.ExecuteTemplate(buf, name, data)
	}
	if err != nil {
		return fmt.Errorf("render: execute template: %w", err)
	}
	setContentType(w, contentTypeHTML)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write html: %w", err)
	}
	return nil
}
