package i18n_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
)

func testLocalesFS(t *testing.T) fs.FS {
	t.Helper()
	return os.DirFS("testdata/locales")
}

// echoHandler writes the resolved locale tag and a translated title.
func echoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, _ = w.Write([]byte(i18n.LocaleFrom(ctx).Tag() + "|" + i18n.T(ctx, "app.title")))
	})
}

func serve(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestMiddlewareNilResolverDoesNotPanic pins the "never panics" guarantee
// against a Resolver with a nil fn. Resolver is exported, so a zero value and
// NewResolver(header, nil) both construct one; each resolves nothing and must
// be skipped, not invoked.
func TestMiddlewareNilResolverDoesNotPanic(t *testing.T) {
	t.Parallel()
	b := newBundle(t)

	// A zero-value Resolver and a nil-fn NewResolver ahead of a working one.
	h := b.Middleware(
		i18n.Resolver{},
		i18n.NewResolver("X-Custom", nil),
		i18n.FromHeader("X-Locale"),
	)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Locale", "uk")
	rec := serve(t, h, req)
	assert.Equal(t, "uk|Панель", rec.Body.String(), "the real resolver still wins")
	// A nil-fn resolver reads no header, so it contributes no Vary entry.
	assert.NotContains(t, rec.Header().Values("Vary"), "X-Custom")

	// A chain of only nil-fn resolvers falls through to the bundle default.
	h2 := b.Middleware(i18n.Resolver{}, i18n.NewResolver("X-Custom", nil))(echoHandler())
	rec2 := serve(t, h2, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, b.Default().Tag()+"|Dashboard", rec2.Body.String())
}

func TestMiddlewareDefaultChain(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	h := b.Middleware()(echoHandler())

	t.Run("cookie wins", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
		req.Header.Set("Accept-Language", "vi")
		req.AddCookie(&http.Cookie{Name: "lang", Value: "uk"})
		rec := serve(t, h, req)
		assert.Equal(t, "uk|Панель", rec.Body.String())
	})

	t.Run("query beats accept-language", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
		req.Header.Set("Accept-Language", "vi")
		rec := serve(t, h, req)
		assert.Equal(t, "de|Übersicht", rec.Body.String())
	})

	t.Run("accept-language", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Language", "uk-UA,uk;q=0.9")
		rec := serve(t, h, req)
		assert.Equal(t, "uk|Панель", rec.Body.String())
	})

	t.Run("default", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, h, httptest.NewRequest(http.MethodGet, "/", nil))
		assert.Equal(t, "en|Dashboard", rec.Body.String())
	})

	t.Run("unsupported values fall through", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/?lang=ww-WW", nil)
		req.AddCookie(&http.Cookie{Name: "lang", Value: "zz"})
		req.Header.Set("Accept-Language", "uk")
		rec := serve(t, h, req)
		// Junk cookie and query are ignored; negotiation still works.
		assert.Equal(t, "uk|Панель", rec.Body.String())
	})
}

func TestMiddlewareHeaders(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	h := b.Middleware()(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "uk")
	rec := serve(t, h, req)
	assert.Equal(t, "uk", rec.Header().Get("Content-Language"))
	assert.Contains(t, rec.Header().Values("Vary"), "Accept-Language")

	// Vary is set from the chain, not from which resolver won: a cookie
	// request still varies by Accept-Language, because a request without the
	// cookie would negotiate differently.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "de"})
	rec = serve(t, h, req)
	assert.Equal(t, "de", rec.Header().Get("Content-Language"))
	assert.Contains(t, rec.Header().Values("Vary"), "Accept-Language")
}

func TestMiddlewarePreservesExistingVary(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	// The inner handler must not touch Vary itself — otherwise it would
	// re-introduce "Cookie" independently of the middleware, and the test
	// would pass even if the middleware used Header.Set instead of Add.
	h := b.Middleware()(echoHandler())

	// Pre-set Vary on the recorder BEFORE the middleware runs, simulating an
	// outer middleware (or a prior response-writing layer) that already
	// declared a Vary dependency.
	rec := httptest.NewRecorder()
	rec.Header().Set("Vary", "Cookie")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "uk")
	h.ServeHTTP(rec, req)

	vary := rec.Header().Values("Vary")
	assert.Contains(t, vary, "Cookie", "a pre-existing Vary value must survive")
	assert.Contains(t, vary, "Accept-Language")
}

func TestMiddlewareNoVaryWithoutAcceptLanguage(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	// A chain that never reads Accept-Language must not Vary on it — an
	// unnecessary Vary costs cache hit rate.
	h := b.Middleware(i18n.FromQuery("lang"))(echoHandler())
	req := httptest.NewRequest(http.MethodGet, "/?lang=uk", nil)
	req.Header.Set("Accept-Language", "de")
	rec := serve(t, h, req)
	assert.Equal(t, "uk|Панель", rec.Body.String())
	assert.NotContains(t, rec.Header().Values("Vary"), "Accept-Language")
}

func TestMiddlewareExplicitChain(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	// An explicit chain overrides the default order entirely.
	h := b.Middleware(i18n.FromAcceptLanguage(), i18n.FromCookie("lang"))(echoHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "de")
	req.AddCookie(&http.Cookie{Name: "lang", Value: "uk"})
	rec := serve(t, h, req)
	assert.Equal(t, "de|Übersicht", rec.Body.String(), "Accept-Language is first in this chain")
}

func TestFromHeader(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	h := b.Middleware(i18n.FromHeader("X-Locale"))(echoHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Locale", "uk")
	rec := serve(t, h, req)
	assert.Equal(t, "uk|Панель", rec.Body.String())
}

func TestNewResolver(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	// A consumer-defined resolver: e.g. an authenticated user's saved choice.
	custom := i18n.NewResolver("", func(bb *i18n.Bundle, r *http.Request) (i18n.Locale, bool) {
		if r.URL.Path == "/uk" {
			return bb.ParseOrDefault("uk"), true
		}
		return i18n.Locale{}, false
	})
	h := b.Middleware(custom, i18n.FromCookie("lang"))(echoHandler())

	rec := serve(t, h, httptest.NewRequest(http.MethodGet, "/uk", nil))
	assert.Equal(t, "uk|Панель", rec.Body.String())

	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "de"})
	rec = serve(t, h, req)
	assert.Equal(t, "de|Übersicht", rec.Body.String())
}

func TestMiddlewareDisabledResolvers(t *testing.T) {
	t.Parallel()
	// Empty CookieName/QueryParam disable those resolvers in the default chain.
	b, err := i18n.New(
		i18n.WithConfig(i18n.Config{DefaultLocale: "en"}),
		i18n.WithMessages(testLocalesFS(t)),
	)
	require.NoError(t, err)
	h := b.Middleware()(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "de"})
	req.Header.Set("Accept-Language", "uk")
	rec := serve(t, h, req)
	// Cookie and query are off; Accept-Language decides.
	assert.Equal(t, "uk|Панель", rec.Body.String())
}

func TestMiddlewareQueryDoesNotConsumeBody(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	h := b.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4)
		n, _ := r.Body.Read(body)
		_, _ = w.Write([]byte(i18n.LocaleFrom(r.Context()).Tag() + "|" + string(body[:n])))
	}))
	// The query resolver must read only the URL, never ParseForm (which would
	// consume the request body out from under the handler).
	req := httptest.NewRequest(http.MethodPost, "/?lang=uk", strings.NewReader("body"))
	rec := serve(t, h, req)
	assert.Equal(t, "uk|body", rec.Body.String())
}

func TestMiddlewareNeverPanicsOnMalformedCookie(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	h := b.Middleware()(echoHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// A raw, unparseable Cookie header must not panic the resolver chain; it
	// must simply fall through to the next source.
	req.Header.Set("Cookie", "lang")
	req.Header.Set("Accept-Language", "uk")
	assert.NotPanics(t, func() {
		rec := serve(t, h, req)
		assert.Equal(t, "uk|Панель", rec.Body.String())
	})
}
