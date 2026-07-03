package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/render"
)

func TestText(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Text(rec, http.StatusOK, "pong")
	require.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "pong", rec.Body.String())
}

func TestBlob_WithContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Blob(rec, http.StatusOK, "image/png", []byte{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, []byte{1, 2, 3}, rec.Body.Bytes())
}

func TestBlob_EmptyContentTypeNotSet(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Blob(rec, http.StatusOK, "", []byte("data"))
	require.NoError(t, err)
	assert.Empty(t, rec.Header().Get("Content-Type")) // we don't set it; left to sniffing
}

func TestNoContent(t *testing.T) {
	rec := httptest.NewRecorder()
	render.NoContent(rec)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}
