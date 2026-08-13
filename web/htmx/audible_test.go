package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/htmx"
)

func audibleRequest(hx bool) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay", nil)
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	return r
}

func runAudible(t *testing.T, h http.Handler, hx bool, opts ...htmx.AudibleOption) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	htmx.NewAudible(opts...)(h).ServeHTTP(rec, audibleRequest(hx))
	return rec
}

func redirectHandler(status int, location string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", location)
		w.WriteHeader(status)
	})
}

func statusHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// TestAudibleRewritesRedirects is the case htmx drops on the floor: it follows no
// redirect, so a 303 leaves the page untouched and the reader clicks again.
func TestAudibleRewritesRedirects(t *testing.T) {
	for _, status := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		rec := runAudible(t, redirectHandler(status, "/invoices"), true)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "/invoices", rec.Header().Get("HX-Redirect"))
		assert.Empty(t, rec.Header().Get("Location"))
	}
}

func TestAudibleLeavesNonHTMXRequestsAlone(t *testing.T) {
	rec := runAudible(t, redirectHandler(http.StatusSeeOther, "/invoices"), false)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/invoices", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
}

func TestAudiblePassesThroughUnregisteredStatuses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusTooManyRequests} {
		rec := runAudible(t, statusHandler(status, "body"), true)
		assert.Equal(t, status, rec.Code)
		assert.Equal(t, "body", rec.Body.String())
	}
}

func TestAudibleRedirectWithoutLocationPassesThrough(t *testing.T) {
	rec := runAudible(t, statusHandler(http.StatusSeeOther, "no location"), true)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "no location", rec.Body.String())
}

func TestAudibleToastReplacesTheBody(t *testing.T) {
	fragment := []byte(`<div id="toast" hx-swap-oob="true">slow down</div>`)
	rec := runAudible(t, statusHandler(http.StatusTooManyRequests, "problem json"), true,
		htmx.WithRewriter(http.StatusTooManyRequests, htmx.Toast(fragment)))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, string(fragment), rec.Body.String())
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.NotContains(t, rec.Body.String(), "problem json")
}

func TestAudibleRewriterSeesStatusAndBody(t *testing.T) {
	var gotStatus int
	var gotBody string
	rec := runAudible(t, statusHandler(http.StatusInternalServerError, "original"), true,
		htmx.WithRewriter(http.StatusInternalServerError,
			func(w http.ResponseWriter, _ *http.Request, status int, body []byte) {
				gotStatus, gotBody = status, string(body)
				w.WriteHeader(http.StatusOK)
			}))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, http.StatusInternalServerError, gotStatus)
	assert.Equal(t, "original", gotBody)
}

func TestAudibleNilRewriterRemovesTheDefault(t *testing.T) {
	rec := runAudible(t, redirectHandler(http.StatusSeeOther, "/invoices"), true,
		htmx.WithRewriter(http.StatusSeeOther, nil))

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/invoices", rec.Header().Get("Location"))
}

func TestAudibleLastRewriterWins(t *testing.T) {
	rec := runAudible(t, statusHandler(http.StatusTooManyRequests, ""), true,
		htmx.WithRewriter(http.StatusTooManyRequests, htmx.Toast([]byte("first"))),
		htmx.WithRewriter(http.StatusTooManyRequests, htmx.Toast([]byte("second"))))

	assert.Equal(t, "second", rec.Body.String())
}

// TestAudibleKeepsTheFirstStatus mirrors net/http: a handler that calls WriteHeader
// twice only commits the first status, so the rewrite keys off that one.
func TestAudibleKeepsTheFirstStatus(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.WriteHeader(http.StatusOK)
	})
	rec := runAudible(t, h, true, htmx.WithRewriter(http.StatusTooManyRequests, htmx.Toast([]byte("toast"))))
	assert.Equal(t, "toast", rec.Body.String())
}

func TestAudibleImplicitOKPassesThrough(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("no explicit status"))
	})
	rec := runAudible(t, h, true)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no explicit status", rec.Body.String())
}

func TestAudiblePreservesHandlerHeaders(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Trace", "abc")
		w.WriteHeader(http.StatusOK)
	})
	rec := runAudible(t, h, true)
	assert.Equal(t, "abc", rec.Header().Get("X-Trace"))
}

func TestAudibleEmptyBodyStatusRewrite(t *testing.T) {
	rec := runAudible(t, statusHandler(http.StatusForbidden, ""), true,
		htmx.WithRewriter(http.StatusForbidden, htmx.Toast([]byte("expired"))))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "expired", rec.Body.String())
	assert.Equal(t, "7", rec.Header().Get("Content-Length"))
}

// TestAudibleKeepsTheResponseControllerChain pins the Unwrap method: without it
// http.ResponseController cannot reach Flush, SetWriteDeadline, or Hijack, and any
// streaming handler under an htmx request loses the capability silently.
func TestAudibleKeepsTheResponseControllerChain(t *testing.T) {
	var isFlusher bool
	var flushErr error
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, isFlusher = w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		flushErr = http.NewResponseController(w).Flush()
	})

	runAudible(t, h, true)
	assert.NoError(t, flushErr)
	assert.False(t, isFlusher, "the buffer must not satisfy Flusher directly; the controller resolves it via Unwrap")
}
