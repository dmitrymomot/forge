package render_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestStream_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Stream(rec, http.StatusOK, "text/plain", strings.NewReader("hello"))
	require.NoError(t, err)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Equal(t, "hello", rec.Body.String())
}

func TestAttachment_DefaultsOctetAndSetsDisposition(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Attachment(rec, http.StatusOK, "f.bin", "", strings.NewReader("x"))
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="f.bin"; filename*=UTF-8''f.bin`,
		rec.Header().Get("Content-Disposition"))
	assert.Equal(t, "x", rec.Body.String())
}

func TestStream_ReaderErrorPropagates(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Stream(rec, http.StatusOK, "text/plain", errReader{})
	require.Error(t, err)
}
