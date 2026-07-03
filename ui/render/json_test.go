package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ui/render"
)

func TestJSON_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSON(rec, http.StatusCreated, map[string]int{"n": 1})
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"n":1}`, rec.Body.String())
}

func TestJSON_TransactionalFailureWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSON(rec, http.StatusAccepted, make(chan int)) // unmarshalable
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // status NOT committed (recorder default)
	assert.Empty(t, rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestJSON_Nil(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSON(rec, http.StatusOK, nil)
	require.NoError(t, err)
	assert.Equal(t, "null\n", rec.Body.String())
}

func TestJSONStream_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSONStream(rec, http.StatusOK, map[string]int{"n": 1})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"n":1}`, rec.Body.String())
}

func TestJSONStream_PassThroughCommitsStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSONStream(rec, http.StatusAccepted, make(chan int))
	require.Error(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code) // status WAS committed before the failure
}
