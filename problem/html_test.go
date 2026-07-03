package problem_test

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/problem"
	"github.com/dmitrymomot/forge/render"
)

func TestHTMLResponder(t *testing.T) {
	tmpl := template.Must(template.New("err").Parse(`<h1>{{.Status}} {{.Title}}</h1>`))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.HTML(tmpl, "")(rec, req, errors.New("bad"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "<h1>400 Bad Request</h1>")
}

// comp is a minimal render.Component built from a Problem.
type comp struct{ p problem.Problem }

func (c comp) Render(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, "component:"+http.StatusText(c.p.Status))
	return err
}

func TestComponentResponder(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.Component(func(p problem.Problem) render.Component { return comp{p} })(rec, req, errors.New("bad"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "component:Bad Request")
}

func TestHTMLNilTemplateStillWritesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.HTML(nil, "", problem.WithStatus(http.StatusInternalServerError))(rec, req, errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code) // not a silent 200
}

func TestComponentNilStillWritesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.Component(func(problem.Problem) render.Component { return nil }, problem.WithStatus(http.StatusInternalServerError))(rec, req, errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
