package render_test

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ui/render"
)

func TestContentDisposition_FilenameSafety(t *testing.T) {
	tests := []struct {
		name, filename, want string
	}{
		{
			name:     "ascii",
			filename: "report.csv",
			want:     `attachment; filename="report.csv"; filename*=UTF-8''report.csv`,
		},
		{
			name:     "unicode each byte underscored in fallback",
			filename: "résumé.csv",
			want:     `attachment; filename="r__sum__.csv"; filename*=UTF-8''r%C3%A9sum%C3%A9.csv`,
		},
		{
			name:     "strips path components",
			filename: "../../etc/passwd",
			want:     `attachment; filename="passwd"; filename*=UTF-8''passwd`,
		},
		{
			name:     "quote and crlf injection neutralized",
			filename: "a\"b\r\nc.csv",
			want:     `attachment; filename="a_b__c.csv"; filename*=UTF-8''a%22b%0D%0Ac.csv`,
		},
		{
			name:     "path-separator-only filename gives no filename params",
			filename: "/",
			want:     "attachment",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := render.Attachment(rec, http.StatusOK, tt.filename,
				"application/octet-stream", strings.NewReader("x"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, rec.Header().Get("Content-Disposition"))
		})
	}
}

func TestContentType_PresetWinsAcrossWriters(t *testing.T) {
	const custom = "text/plain; charset=iso-8859-1"

	t.Run("JSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", custom)
		require.NoError(t, render.JSON(rec, http.StatusOK, 1))
		assert.Equal(t, custom, rec.Header().Get("Content-Type"))
	})
	t.Run("Text", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", custom)
		require.NoError(t, render.Text(rec, http.StatusOK, "x"))
		assert.Equal(t, custom, rec.Header().Get("Content-Type"))
	})
	t.Run("HTML", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", custom)
		tmpl := template.Must(template.New("x").Parse("hi"))
		require.NoError(t, render.HTML(rec, http.StatusOK, tmpl, "", nil))
		assert.Equal(t, custom, rec.Header().Get("Content-Type"))
	})
}

// failWriter is an http.ResponseWriter whose Write always fails.
type failWriter struct {
	header http.Header
	code   int
}

func newFailWriter() *failWriter           { return &failWriter{header: make(http.Header)} }
func (f *failWriter) Header() http.Header  { return f.header }
func (f *failWriter) WriteHeader(code int) { f.code = code }
func (f *failWriter) Write([]byte) (int, error) {
	return 0, errors.New("connection reset")
}

func TestWriteErrorPropagates(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		require.Error(t, render.JSON(newFailWriter(), http.StatusOK, map[string]int{"n": 1}))
	})
	t.Run("Text", func(t *testing.T) {
		require.Error(t, render.Text(newFailWriter(), http.StatusOK, "x"))
	})
	t.Run("Blob", func(t *testing.T) {
		require.Error(t, render.Blob(newFailWriter(), http.StatusOK, "text/plain", []byte("x")))
	})
	t.Run("Stream", func(t *testing.T) {
		require.Error(t, render.Stream(newFailWriter(), http.StatusOK, "text/plain", strings.NewReader("x")))
	})
	t.Run("CSV", func(t *testing.T) {
		require.Error(t, render.CSV(newFailWriter(), http.StatusOK, "", [][]string{{"a"}}))
	})
	t.Run("HTML", func(t *testing.T) {
		tmpl := template.Must(template.New("x").Parse("hi"))
		require.Error(t, render.HTML(newFailWriter(), http.StatusOK, tmpl, "", nil))
	})
	t.Run("Templ", func(t *testing.T) {
		require.Error(t, render.Templ(context.Background(), newFailWriter(), http.StatusOK, &fakeComponent{out: "hi"}))
	})
}
