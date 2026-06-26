package request_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/request"
)

func multipartReq(t *testing.T, field, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = fw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	r := httptest.NewRequest(http.MethodPost, "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func TestRawBody(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	b, err := request.RawBody(r)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))
}

func TestRawBodyTooLarge(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789"))
	_, err := request.RawBody(r, request.WithMaxBytes(4))
	require.Error(t, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, request.StatusCode(err))
}

func TestFile(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "doc", "a.txt", "filedata")

	f, h, err := request.File(r, "doc")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	assert.Equal(t, "a.txt", h.Filename)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "filedata", string(data))
}

func TestFileMissing(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "other", "x.txt", "x")
	_, _, err := request.File(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestFileNonMultipart(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	r.Header.Set("Content-Type", "application/json")
	_, _, err := request.File(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, request.StatusCode(err))
}

func TestFiles(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "doc", "a.txt", "data")
	headers, err := request.Files(r, "doc")
	require.NoError(t, err)
	require.Len(t, headers, 1)
	assert.Equal(t, "a.txt", headers[0].Filename)
}

func TestFilesMissing(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "other", "x.txt", "x")
	_, err := request.Files(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestFilesNonMultipart(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	r.Header.Set("Content-Type", "application/json")
	_, err := request.Files(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, request.StatusCode(err))
}

func TestFileFilesMissingSentinel(t *testing.T) {
	t.Parallel()
	// Both File and Files surface http.ErrMissingFile for a missing key.
	r1 := multipartReq(t, "other", "x.txt", "x")
	_, _, err := request.File(r1, "doc")
	assert.ErrorIs(t, err, http.ErrMissingFile)

	r2 := multipartReq(t, "other", "x.txt", "x")
	_, err = request.Files(r2, "doc")
	assert.ErrorIs(t, err, http.ErrMissingFile)
}

func TestFileMissingKind(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "other", "x.txt", "x")
	_, _, err := request.File(r, "doc")

	var re *request.Error
	require.ErrorAs(t, err, &re)
	require.NotNil(t, re)
	if re != nil {
		assert.Equal(t, request.KindMissing, re.Kind) // absent, not malformed
	}
}
