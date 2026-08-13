package respond_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/respond"
)

func serve(t *testing.T, res respond.Response, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	require.NoError(t, res.Respond(rec, req))
	return rec
}

func get() *http.Request { return httptest.NewRequest(http.MethodGet, "/", nil) }

func htmxGet() *http.Request {
	r := get()
	r.Header.Set("HX-Request", "true")
	return r
}

func TestText(t *testing.T) {
	rec := serve(t, respond.Text("hello"), get())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
}

func TestJSON(t *testing.T) {
	rec := serve(t, respond.JSON(map[string]int{"n": 1}), get())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"n":1}`, rec.Body.String())
}

func TestBlobAndStream(t *testing.T) {
	rec := serve(t, respond.Blob("text/csv", []byte("a,b")), get())
	assert.Equal(t, "text/csv", rec.Header().Get("Content-Type"))
	assert.Equal(t, "a,b", rec.Body.String())

	rec = serve(t, respond.Stream("text/plain", strings.NewReader("streamed")), get())
	assert.Equal(t, "streamed", rec.Body.String())
}

func TestAttachmentNamesTheDownload(t *testing.T) {
	rec := serve(t, respond.Attachment("report.txt", "text/plain", strings.NewReader("x")), get())
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "report.txt")
}

func TestCSV(t *testing.T) {
	rec := serve(t, respond.CSV("rows.csv", [][]string{{"a", "b"}, {"1", "2"}}), get())
	assert.Contains(t, rec.Body.String(), "a,b")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "rows.csv")
}

func TestNoContent(t *testing.T) {
	rec := serve(t, respond.NoContent(), get())
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestFileFS(t *testing.T) {
	fsys := fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("file body")}}
	rec := serve(t, respond.FileFS(fsys, "a.txt"), get())
	assert.Equal(t, "file body", rec.Body.String())
}

func TestWithStatusAndHeaders(t *testing.T) {
	rec := serve(t, respond.Text("created",
		respond.WithStatus(http.StatusCreated),
		respond.WithHeader("X-Trace", "one"),
		respond.WithHeader("X-Trace", "two"),
		respond.WithAddedHeader("X-Tag", "a"),
		respond.WithAddedHeader("X-Tag", "b"),
	), get())

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "two", rec.Header().Get("X-Trace"))
	assert.Equal(t, []string{"a", "b"}, rec.Header().Values("X-Tag"))
}

// TestSeeOtherSpeaksHTMX is the branch that exists once here instead of in every
// handler that redirects: htmx swaps nothing on a 303, so it gets 200 + HX-Redirect.
func TestSeeOtherSpeaksHTMX(t *testing.T) {
	browser := serve(t, respond.SeeOther("/invoices"), get())
	assert.Equal(t, http.StatusSeeOther, browser.Code)
	assert.Equal(t, "/invoices", browser.Header().Get("Location"))
	assert.Empty(t, browser.Header().Get("HX-Redirect"))

	htmxRec := serve(t, respond.SeeOther("/invoices"), htmxGet())
	assert.Equal(t, http.StatusOK, htmxRec.Code)
	assert.Equal(t, "/invoices", htmxRec.Header().Get("HX-Redirect"))
	assert.Empty(t, htmxRec.Header().Get("Location"))
}

func TestFoundUses302(t *testing.T) {
	rec := serve(t, respond.Found("/login"), get())
	assert.Equal(t, http.StatusFound, rec.Code)
}

func TestExternalRedirect(t *testing.T) {
	rec := serve(t, respond.External("https://example.com/pay"), get())
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "https://example.com/pay", rec.Header().Get("Location"))
}

func TestRaw(t *testing.T) {
	rec := serve(t, respond.Raw(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusTeapot)
		_, err := io.WriteString(w, "owned")
		return err
	}), get())

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "owned", rec.Body.String())
}

func TestRawWithoutWriterErrors(t *testing.T) {
	err := respond.Raw(nil).Respond(httptest.NewRecorder(), get())
	assert.ErrorIs(t, err, respond.ErrNoWriter)
}

func TestStatusOf(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, respond.StatusOf(respond.ErrNotFound))
	assert.Equal(t, http.StatusMethodNotAllowed, respond.StatusOf(respond.ErrMethodNotAllowed))
	assert.Equal(t, http.StatusInternalServerError, respond.StatusOf(respond.ErrNoResponse))
	assert.Equal(t, http.StatusInternalServerError, respond.StatusOf(respond.ErrNoWriter))
	assert.Equal(t, 0, respond.StatusOf(errors.New("other")))
	assert.Equal(t, 0, respond.StatusOf(nil))
}

// TestWithAddedHeaderJoinsWhatIsAlreadySet pins the difference from WithHeader: an
// added value must not drop what an outer middleware already put on the response.
func TestWithAddedHeaderJoinsWhatIsAlreadySet(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Add("Vary", "Accept-Encoding")
	require.NoError(t, respond.Text("ok", respond.WithAddedHeader("Vary", "HX-Request")).Respond(rec, get()))

	assert.Equal(t, []string{"Accept-Encoding", "HX-Request"}, rec.Header().Values("Vary"))
}

// TestWithHeaderReplacesWhatIsAlreadySet is the other half of the pair: Set means set.
func TestWithHeaderReplacesWhatIsAlreadySet(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Add("Vary", "Accept-Encoding")
	require.NoError(t, respond.Text("ok", respond.WithHeader("Vary", "HX-Request")).Respond(rec, get()))

	assert.Equal(t, []string{"HX-Request"}, rec.Header().Values("Vary"))
}
