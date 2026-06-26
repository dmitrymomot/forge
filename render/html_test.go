package render_test

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestHTML_Execute(t *testing.T) {
	tmpl := template.Must(template.New("x").Parse("Hi {{.}}"))
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusOK, tmpl, "", "Bob")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "Hi Bob", rec.Body.String())
}

func TestHTML_ExecuteTemplateNamed(t *testing.T) {
	tmpl := template.Must(template.New("root").Parse(`{{define "page"}}P:{{.}}{{end}}`))
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusCreated, tmpl, "page", "x")
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code) // status committed verbatim on the named-template path
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "P:x", rec.Body.String())
}

func TestHTML_NilTemplate(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusOK, nil, "", nil)
	require.ErrorIs(t, err, render.ErrNilTemplate)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestHTML_ExecuteErrorWritesNothing(t *testing.T) {
	tmpl := template.Must(template.New("x").Funcs(template.FuncMap{
		"boom": func() (string, error) { return "", errors.New("boom") },
	}).Parse("{{boom}}"))
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusAccepted, tmpl, "", nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // not committed
	assert.Empty(t, rec.Body.String())
}
