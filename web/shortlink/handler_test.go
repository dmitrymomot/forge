package shortlink_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/shortlink"
)

func newHandlerFixture(t *testing.T, opts ...shortlink.Option) (*shortlink.Manager, shortlink.Link) {
	t.Helper()
	mgr := shortlink.New(shortlink.NewMemoryStore(), opts...)
	l, err := mgr.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com/dest"})
	require.NoError(t, err)
	return mgr, l
}

func serve(h http.Handler, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle("GET /{code}", h)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestHandler_Redirect(t *testing.T) {
	t.Parallel()
	mgr, l := newHandlerFixture(t)

	w := serve(mgr.Handler(), "/"+l.Code)
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://example.com/dest", w.Header().Get("Location"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestHandler_RedirectStatus307(t *testing.T) {
	t.Parallel()
	mgr, l := newHandlerFixture(t, shortlink.WithRedirectStatus(http.StatusTemporaryRedirect))

	w := serve(mgr.Handler(), "/"+l.Code)
	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
}

func TestHandler_NotFound(t *testing.T) {
	t.Parallel()
	mgr, _ := newHandlerFixture(t)

	w := serve(mgr.Handler(), "/does-not-exist")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestHandler_Fallback(t *testing.T) {
	t.Parallel()
	mgr, l := newHandlerFixture(t, shortlink.WithFallbackURL("https://example.com/link-gone"))
	require.NoError(t, mgr.Deactivate(context.Background(), l.Code))

	for _, target := range []string{"/" + l.Code, "/unknown-code"} {
		w := serve(mgr.Handler(), target)
		assert.Equal(t, http.StatusFound, w.Code, "target %s", target)
		assert.Equal(t, "https://example.com/link-gone", w.Header().Get("Location"), "target %s", target)
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "target %s", target)
	}
}

func TestHandler_PathFallbackWithoutPattern(t *testing.T) {
	t.Parallel()
	mgr, l := newHandlerFixture(t)

	// Mounted bare (no {code} pattern), the handler trims the path itself.
	w := httptest.NewRecorder()
	mgr.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+l.Code, nil))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://example.com/dest", w.Header().Get("Location"))

	// A bare "/" resolves nothing.
	w = httptest.NewRecorder()
	mgr.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_ExpiredUsesFallback(t *testing.T) {
	t.Parallel()
	mgr := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithFallbackURL("/gone"))

	l, err := mgr.Create(context.Background(), shortlink.CreateParams{
		URL: "https://example.com", ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	require.NoError(t, err)

	w := serve(mgr.Handler(), "/"+l.Code)
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/gone", w.Header().Get("Location"))
}

// erroringStore fails every Get with a non-sentinel error, simulating a
// backend outage.
type erroringStore struct{ shortlink.Store }

func (erroringStore) Get(context.Context, string) (shortlink.Link, error) {
	return shortlink.Link{}, errors.New("connection refused")
}

func TestHandler_BackendErrorIs500(t *testing.T) {
	t.Parallel()
	mgr := shortlink.New(erroringStore{shortlink.NewMemoryStore()},
		shortlink.WithFallbackURL("https://example.com/link-gone"))

	// An outage must surface as 500, never as a dead-link fallback or 404.
	w := serve(mgr.Handler(), "/anycode")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
}

func TestHandler_OnHitFires(t *testing.T) {
	t.Parallel()
	hits := 0
	mgr, l := newHandlerFixture(t, shortlink.WithOnHit(func(context.Context, shortlink.Link) { hits++ }))

	serve(mgr.Handler(), "/"+l.Code)
	assert.Equal(t, 1, hits)

	serve(mgr.Handler(), "/missing")
	assert.Equal(t, 1, hits, "failed redirects must not count as hits")
}
