package requestid_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/requestid"
)

func serve(mw func(http.Handler) http.Handler, r *http.Request, h http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, r)
	return rec
}

func TestGeneratesWhenAbsent(t *testing.T) {
	var seen string
	rec := serve(requestid.New(), httptest.NewRequest(http.MethodGet, "/", nil),
		func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotEmpty(t, seen)
	assert.Equal(t, seen, rec.Header().Get("X-Request-ID"))
}

func TestTrustsValidInbound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	var seen string
	serve(requestid.New(), req, func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.Equal(t, "abc-123", seen)
}

func TestRejectsOversizedInbound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("a", 129))
	var seen string
	serve(requestid.New(), req, func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotEqual(t, strings.Repeat("a", 129), seen)
	assert.NotEmpty(t, seen) // generated instead
}

func TestRejectsNonASCIIInbound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "id\nwith\rnewline")
	var seen string
	serve(requestid.New(), req, func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotContains(t, seen, "\n")
}

func TestTrustInboundDisabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "client-supplied")
	var seen string
	serve(requestid.New(requestid.WithTrustInbound(false)), req,
		func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotEqual(t, "client-supplied", seen)
}

func TestCustomHeaderAndGenerator(t *testing.T) {
	var seen string
	rec := serve(
		requestid.New(requestid.WithHeader("X-Trace"), requestid.WithGenerator(func() string { return "fixed" })),
		httptest.NewRequest(http.MethodGet, "/", nil),
		func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) },
	)
	assert.Equal(t, "fixed", seen)
	assert.Equal(t, "fixed", rec.Header().Get("X-Trace"))
}

func TestLogExtractor(t *testing.T) {
	var attrOK bool
	serve(requestid.New(requestid.WithGenerator(func() string { return "rid-1" })),
		httptest.NewRequest(http.MethodGet, "/", nil),
		func(w http.ResponseWriter, r *http.Request) {
			attr, ok := requestid.LogExtractor(r.Context())
			attrOK = ok
			assert.Equal(t, "rid-1", attr.Value.String())
		})
	assert.True(t, attrOK)
}
