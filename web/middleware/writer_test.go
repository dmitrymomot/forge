package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/middleware"
)

func TestWrapWriterCapturesStatusAndBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := middleware.WrapWriter(rec)
	assert.False(t, rw.Wrote())
	assert.Equal(t, 0, rw.Status())

	rw.WriteHeader(http.StatusTeapot)
	n, err := rw.Write([]byte("hello"))
	require.NoError(t, err)

	assert.Equal(t, 5, n)
	assert.True(t, rw.Wrote())
	assert.Equal(t, http.StatusTeapot, rw.Status())
	assert.Equal(t, int64(5), rw.Written())
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

func TestWrapWriterImplicit200(t *testing.T) {
	rw := middleware.WrapWriter(httptest.NewRecorder())
	_, _ = rw.Write([]byte("x"))
	assert.Equal(t, http.StatusOK, rw.Status())
}

func TestWrapWriterIdempotent(t *testing.T) {
	rw := middleware.WrapWriter(httptest.NewRecorder())
	assert.Same(t, rw, middleware.WrapWriter(rw))
}

func TestWrapWriterUnwrapReachesFlusher(t *testing.T) {
	rec := httptest.NewRecorder() // implements http.Flusher
	rw := middleware.WrapWriter(rec)
	// http.ResponseController uses Unwrap to reach the underlying Flusher.
	require.NoError(t, http.NewResponseController(rw).Flush())
	assert.True(t, rec.Flushed)
}

func TestWriteHeaderOnlyOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := middleware.WrapWriter(rec)
	rw.WriteHeader(http.StatusCreated)
	rw.WriteHeader(http.StatusBadRequest) // ignored
	assert.Equal(t, http.StatusCreated, rw.Status())
}
