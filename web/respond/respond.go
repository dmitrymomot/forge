package respond

import (
	"html/template"
	"io"
	"io/fs"
	"net/http"

	"github.com/dmitrymomot/forge/web/htmx"
	"github.com/dmitrymomot/forge/web/render"
)

// Response is an answer a handler decided on and has not written yet.
type Response interface {
	Respond(w http.ResponseWriter, r *http.Request) error
}

// Handler answers with a Response or an error and never writes, so the edge decides
// how each answer reaches this client. Responder.Wrap turns it into an http.Handler.
type Handler func(r *http.Request) (Response, error)

// Component is a renderable component (a-h/templ and anything with the same shape).
type Component = render.Component

type textResponse struct {
	body string
	config
}

// Text answers with plain text.
func Text(body string, opts ...Option) Response {
	return textResponse{config: newConfig(http.StatusOK, opts), body: body}
}

func (t textResponse) Respond(w http.ResponseWriter, _ *http.Request) error {
	t.applyHeaders(w)
	return render.Text(w, t.status, t.body)
}

type jsonResponse struct {
	value any
	config
}

// JSON answers with the value encoded as JSON.
func JSON(value any, opts ...Option) Response {
	return jsonResponse{config: newConfig(http.StatusOK, opts), value: value}
}

func (j jsonResponse) Respond(w http.ResponseWriter, _ *http.Request) error {
	j.applyHeaders(w)
	return render.JSON(w, j.status, j.value)
}

type templResponse struct {
	components []Component
	config
}

// Templ answers with one rendered component.
func Templ(c Component, opts ...Option) Response {
	return templResponse{config: newConfig(http.StatusOK, opts), components: []Component{c}}
}

// Components answers with several components in one body, which is how an htmx
// response pairs its target fragment with out-of-band fragments.
func Components(cs []Component, opts ...Option) Response {
	return templResponse{config: newConfig(http.StatusOK, opts), components: cs}
}

func (t templResponse) Respond(w http.ResponseWriter, r *http.Request) error {
	t.applyHeaders(w)
	if len(t.components) == 1 {
		return render.Templ(r.Context(), w, t.status, t.components[0])
	}
	return render.Components(r.Context(), w, t.status, t.components...)
}

type htmlResponse struct {
	data any
	tmpl *template.Template
	name string
	config
}

// HTML answers with one template of a parsed set. An empty name executes the
// template itself.
func HTML(tmpl *template.Template, name string, data any, opts ...Option) Response {
	return htmlResponse{config: newConfig(http.StatusOK, opts), tmpl: tmpl, name: name, data: data}
}

func (h htmlResponse) Respond(w http.ResponseWriter, _ *http.Request) error {
	h.applyHeaders(w)
	return render.HTML(w, h.status, h.tmpl, h.name, h.data)
}

type blobResponse struct {
	contentType string
	body        []byte
	config
}

// Blob answers with raw bytes under contentType.
func Blob(contentType string, body []byte, opts ...Option) Response {
	return blobResponse{config: newConfig(http.StatusOK, opts), contentType: contentType, body: body}
}

func (b blobResponse) Respond(w http.ResponseWriter, _ *http.Request) error {
	b.applyHeaders(w)
	return render.Blob(w, b.status, b.contentType, b.body)
}

type streamResponse struct {
	body        io.Reader
	contentType string
	filename    string
	config
}

// Stream answers with the reader's bytes under contentType.
func Stream(contentType string, body io.Reader, opts ...Option) Response {
	return streamResponse{config: newConfig(http.StatusOK, opts), contentType: contentType, body: body}
}

// Attachment is Stream with a Content-Disposition that names the download.
func Attachment(filename, contentType string, body io.Reader, opts ...Option) Response {
	return streamResponse{
		config:      newConfig(http.StatusOK, opts),
		contentType: contentType,
		filename:    filename,
		body:        body,
	}
}

func (s streamResponse) Respond(w http.ResponseWriter, _ *http.Request) error {
	s.applyHeaders(w)
	if s.filename != "" {
		return render.Attachment(w, s.status, s.filename, s.contentType, s.body)
	}
	return render.Stream(w, s.status, s.contentType, s.body)
}

type csvResponse struct {
	filename string
	records  [][]string
	config
}

// CSV answers with the records as a CSV body, named by filename when non-empty.
func CSV(filename string, records [][]string, opts ...Option) Response {
	return csvResponse{config: newConfig(http.StatusOK, opts), filename: filename, records: records}
}

func (c csvResponse) Respond(w http.ResponseWriter, _ *http.Request) error {
	c.applyHeaders(w)
	return render.CSV(w, c.status, c.filename, c.records)
}

type redirectResponse struct {
	url string
	config
	external bool
}

// SeeOther sends the reader elsewhere after a write, which is the redirect half of
// post/redirect/get. It speaks the client's language: htmx receives 200 and
// HX-Redirect, because it follows no 303; a browser receives the status and Location.
func SeeOther(url string, opts ...Option) Response {
	return redirectResponse{config: newConfig(http.StatusSeeOther, opts), url: url}
}

// Found is SeeOther with 302, for a redirect that is not the result of a write.
func Found(url string, opts ...Option) Response {
	return redirectResponse{config: newConfig(http.StatusFound, opts), url: url}
}

// External redirects off-site with a full page load. HX-Location is an AJAX swap and
// only works same-origin, so an external destination must not use the htmx path.
func External(url string, opts ...Option) Response {
	return redirectResponse{config: newConfig(http.StatusSeeOther, opts), url: url, external: true}
}

func (rd redirectResponse) Respond(w http.ResponseWriter, r *http.Request) error {
	rd.applyHeaders(w)
	if rd.external {
		htmx.RedirectExternal(w, r, rd.url, rd.status)
		return nil
	}
	htmx.Redirect(w, r, rd.url, rd.status)
	return nil
}

type fileResponse struct {
	fsys fs.FS
	name string
	config
}

// File answers with one file from the filesystem, range requests included.
func File(path string, opts ...Option) Response {
	return fileResponse{config: newConfig(http.StatusOK, opts), name: path}
}

// FileFS answers with one file of fsys, range requests included.
func FileFS(fsys fs.FS, name string, opts ...Option) Response {
	return fileResponse{config: newConfig(http.StatusOK, opts), fsys: fsys, name: name}
}

func (f fileResponse) Respond(w http.ResponseWriter, r *http.Request) error {
	f.applyHeaders(w)
	if f.fsys == nil {
		render.File(w, r, f.name)
		return nil
	}
	render.FileFS(w, r, f.fsys, f.name)
	return nil
}

type noContentResponse struct {
	config
}

// NoContent answers that the write landed and there is nothing to show.
func NoContent(opts ...Option) Response {
	return noContentResponse{config: newConfig(http.StatusNoContent, opts)}
}

func (n noContentResponse) Respond(w http.ResponseWriter, _ *http.Request) error {
	n.applyHeaders(w)
	render.NoContent(w)
	return nil
}

type rawResponse struct {
	write func(w http.ResponseWriter, r *http.Request) error
	config
}

// Raw hands the writer to a func that must own it — a hijack, an SSE loop, a body
// this package has no shape for. It is deliberately more ceremony than the others so
// a reader sees where the value-returning contract stops.
func Raw(write func(w http.ResponseWriter, r *http.Request) error, opts ...Option) Response {
	return rawResponse{config: newConfig(http.StatusOK, opts), write: write}
}

func (rw rawResponse) Respond(w http.ResponseWriter, r *http.Request) error {
	rw.applyHeaders(w)
	if rw.write == nil {
		return ErrNoWriter
	}
	return rw.write(w, r)
}
