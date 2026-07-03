package render_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/render"
)

func TestFile_ServesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o600))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hello.txt", nil)
	render.File(rec, req, path)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello world", rec.Body.String())
}

func TestFileFS_ServesFromFS(t *testing.T) {
	fsys := fstest.MapFS{"logo.svg": &fstest.MapFile{Data: []byte("<svg/>")}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logo.svg", nil)
	render.FileFS(rec, req, fsys, "logo.svg")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<svg/>", rec.Body.String())
}

func TestFile_RangeRequestReturns206(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	require.NoError(t, os.WriteFile(path, []byte("0123456789"), 0o600))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data.txt", nil)
	req.Header.Set("Range", "bytes=0-3")
	render.File(rec, req, path)
	assert.Equal(t, http.StatusPartialContent, rec.Code) // proves stdlib delegation
	assert.Equal(t, "0123", rec.Body.String())
}
