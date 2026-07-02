package problem_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/problem"
)

func TestTextResponderDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.Text()(rec, req, errors.New("bad input"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rec.Body.String(), "400 Bad Request")
	assert.Contains(t, rec.Body.String(), "bad input")
}

func TestTextResponder5xxOmitsErrorText(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.Text(problem.WithStatus(http.StatusInternalServerError))(rec, req, errors.New("secret dsn"))

	assert.NotContains(t, rec.Body.String(), "secret dsn")
	assert.Contains(t, rec.Body.String(), "500 Internal Server Error")
}

func TestTextWithTemplate(t *testing.T) {
	tmpl := template.Must(template.New("p").Parse(`ERR {{.Status}}`))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.Text(problem.WithTemplate(tmpl))(rec, req, errors.New("x"))
	assert.Equal(t, "ERR 400", rec.Body.String())
}
