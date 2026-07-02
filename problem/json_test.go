package problem_test

import (
	"encoding/json"
	"errors"
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
