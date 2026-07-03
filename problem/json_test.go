package problem_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/problem"
)

func TestJSONResponderContentTypeAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)

	problem.JSON()(rec, req, errors.New("bad input"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

	var p problem.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, http.StatusBadRequest, p.Status)
	assert.Equal(t, "bad input", p.Detail)
	assert.Equal(t, "/widgets/7", p.Instance)
}

func TestJSONResponder5xxOmitsErrorText(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	problem.JSON(problem.WithStatus(http.StatusInternalServerError))(rec, req, errors.New("secret db dsn leaked"))

	assert.NotContains(t, rec.Body.String(), "secret db dsn")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestJSONResponderLogs5xxNotBody(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.JSON(problem.WithLogger(log), problem.WithStatus(http.StatusInternalServerError))(rec, req, errors.New("db dsn secret"))
	assert.Contains(t, buf.String(), "db dsn secret")         // logged for ops
	assert.NotContains(t, rec.Body.String(), "db dsn secret") // never leaked in body
}

func TestJSONResponderDoesNotLog4xx(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	problem.JSON(problem.WithLogger(log))(rec, req, errors.New("bad input")) // resolves to 400
	assert.Empty(t, buf.String())                                            // 4xx is not logged
}
