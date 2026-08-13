package hostrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// captureMatch returns a handler that records the Match seen during the request.
func captureMatch(dst *Match, ok *bool) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		*dst, *ok = FromContext(r.Context())
	})
}

// doServe drives a single request for host through r.
func doServe(r *Router, host string) {
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = host
	r.ServeHTTP(httptest.NewRecorder(), req)
}

func TestFromContext_Wildcard(t *testing.T) {
	var got Match
	var ok bool
	r := mustNew(t, WithHost("*.example.com", captureMatch(&got, &ok)))
	doServe(r, "foo.example.com")

	assert.True(t, ok)
	assert.Equal(t, Match{Host: "foo.example.com", Pattern: "*.example.com", Subdomain: "foo"}, got)
}

func TestFromContext_Exact(t *testing.T) {
	var got Match
	var ok bool
	r := mustNew(t, WithHost("api.example.com", captureMatch(&got, &ok)))
	doServe(r, "api.example.com")

	assert.True(t, ok)
	assert.Equal(t, Match{Host: "api.example.com", Pattern: "api.example.com", Subdomain: ""}, got)
}

func TestFromContext_FallbackHasNoMatch(t *testing.T) {
	var got Match
	var ok bool
	r := mustNew(t, WithFallback(captureMatch(&got, &ok)))
	doServe(r, "unknown.com")

	assert.False(t, ok)
	assert.Equal(t, Match{}, got)
}

func TestAccessors(t *testing.T) {
	var sub, pat, host string
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub, pat, host = Subdomain(ctx), Pattern(ctx), Host(ctx)
	})
	r := mustNew(t, WithHost("*.example.com", h))
	doServe(r, "tenant.example.com")

	assert.Equal(t, "tenant", sub)
	assert.Equal(t, "*.example.com", pat)
	assert.Equal(t, "tenant.example.com", host)
}

func TestWithoutMatchContext_NoInjection(t *testing.T) {
	var got Match
	var ok bool
	r := mustNew(t,
		WithHost("*.example.com", captureMatch(&got, &ok)),
		WithoutMatchContext(),
	)
	doServe(r, "foo.example.com")

	assert.False(t, ok, "injection disabled, FromContext must report no match")
	assert.Equal(t, Match{}, got)
}

func TestMatch_SurvivesDownstreamContextWrap(t *testing.T) {
	type otherKey struct{}
	var got Match
	var ok bool
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// middleware adds its own value under a different key downstream
		wrapped := context.WithValue(r.Context(), otherKey{}, "x")
		got, ok = FromContext(wrapped)
	})
	r := mustNew(t, WithHost("*.example.com", h))
	doServe(r, "foo.example.com")

	assert.True(t, ok)
	assert.Equal(t, "foo", got.Subdomain)
}
