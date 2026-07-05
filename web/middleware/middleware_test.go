package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/middleware"
)

func tag(s string, order *[]string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, s+":in")
			next.ServeHTTP(w, r)
			*order = append(*order, s+":out")
		})
	}
}

func TestWrapOrderOutermostFirst(t *testing.T) {
	var order []string
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
		tag("a", &order), tag("b", &order),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, []string{"a:in", "b:in", "handler", "b:out", "a:out"}, order)
}

func TestWrapEmptyIsIdentity(t *testing.T) {
	called := false
	base := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	middleware.Wrap(base).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, called)
}

func TestChainEqualsWrap(t *testing.T) {
	var order []string
	mw := middleware.Chain(tag("x", &order), tag("y", &order))
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "h") })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, []string{"x:in", "y:in", "h", "y:out", "x:out"}, order)
}

func markHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Marked", "1")
		next.ServeHTTP(w, r)
	})
}

func TestWhen_AppliesOnlyWhenPredicateTrue(t *testing.T) {
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.When(func(r *http.Request) bool { return r.URL.Path == "/on" }, markHeader),
	)
	on := httptest.NewRecorder()
	h.ServeHTTP(on, httptest.NewRequest(http.MethodGet, "/on", nil))
	assert.Equal(t, "1", on.Header().Get("X-Marked"))

	off := httptest.NewRecorder()
	h.ServeHTTP(off, httptest.NewRequest(http.MethodGet, "/off", nil))
	assert.Empty(t, off.Header().Get("X-Marked"))
}

func TestSkip_IsInverseOfWhen(t *testing.T) {
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.Skip(func(r *http.Request) bool { return r.URL.Path == "/skip" }, markHeader),
	)
	skip := httptest.NewRecorder()
	h.ServeHTTP(skip, httptest.NewRequest(http.MethodGet, "/skip", nil))
	assert.Empty(t, skip.Header().Get("X-Marked"))

	apply := httptest.NewRecorder()
	h.ServeHTTP(apply, httptest.NewRequest(http.MethodGet, "/other", nil))
	assert.Equal(t, "1", apply.Header().Get("X-Marked"))
}

func TestWhen_BuildsMiddlewareOnce(t *testing.T) {
	builds := 0
	mw := func(next http.Handler) http.Handler {
		builds++
		return next
	}
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.When(func(*http.Request) bool { return true }, mw),
	)
	for range 3 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	assert.Equal(t, 1, builds)
}
