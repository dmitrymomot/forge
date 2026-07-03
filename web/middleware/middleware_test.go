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
