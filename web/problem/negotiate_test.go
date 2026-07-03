package problem_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/problem"
)

func mark(name string, out *string) problem.Responder {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		*out = name
		w.WriteHeader(http.StatusOK)
	}
}

func negotiator(out *string) problem.Responder {
	return problem.Negotiate(mark("json", out), map[string]problem.Responder{
		"text/html": mark("html", out),
	})
}

func TestNegotiatePicksMappedType(t *testing.T) {
	var called string
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	negotiator(&called)(httptest.NewRecorder(), req, errors.New("x"))
	assert.Equal(t, "html", called)
}

func TestNegotiateFallbackOnNoMatch(t *testing.T) {
	var called string
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	negotiator(&called)(httptest.NewRecorder(), req, errors.New("x"))
	assert.Equal(t, "json", called)
}

func TestNegotiateFallbackOnNoAccept(t *testing.T) {
	var called string
	negotiator(&called)(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), errors.New("x"))
	assert.Equal(t, "json", called)
}

func TestNegotiateMatchIsCaseInsensitive(t *testing.T) {
	var called string
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "Text/HTML") // mixed case must still match the "text/html" key
	negotiator(&called)(httptest.NewRecorder(), req, errors.New("x"))
	assert.Equal(t, "html", called)
}
