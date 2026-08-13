package hostrouter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errStoreDown = errors.New("store down")

// lookupOf returns a LookupFunc serving handler for exactly one host and reporting
// ErrHostNotFound for every other.
func lookupOf(host string, h http.Handler) LookupFunc {
	return func(_ context.Context, got string) (http.Handler, error) {
		if got == host {
			return h, nil
		}
		return nil, ErrHostNotFound
	}
}

func serve(r *Router, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestLookup_ResolvesCustomerDomain(t *testing.T) {
	r := mustNew(t,
		WithLookup(lookupOf("shop.customer.tld", handlerWriting("custom"))),
		WithFallback(handlerWriting("fallback")),
	)
	assert.Equal(t, "custom", serve(r, "shop.customer.tld").Body.String())
}

func TestLookup_RunsAfterStaticPatterns(t *testing.T) {
	called := false
	r := mustNew(t,
		WithHost("api.example.com", handlerWriting("exact")),
		WithHost("*.example.com", handlerWriting("wild")),
		WithLookup(func(context.Context, string) (http.Handler, error) {
			called = true
			return handlerWriting("lookup"), nil
		}),
	)

	assert.Equal(t, "exact", serve(r, "api.example.com").Body.String())
	assert.Equal(t, "wild", serve(r, "foo.example.com").Body.String())
	assert.False(t, called, "a static pattern must never reach the lookup")

	assert.Equal(t, "lookup", serve(r, "other.tld").Body.String())
	assert.True(t, called)
}

func TestLookup_NormalizesHostBeforeResolving(t *testing.T) {
	var seen string
	r := mustNew(t, WithLookup(func(_ context.Context, host string) (http.Handler, error) {
		seen = host
		return nil, ErrHostNotFound
	}))

	serve(r, "SHOP.Customer.TLD:8443")
	assert.Equal(t, "shop.customer.tld", seen)
}

func TestLookup_UnknownHostReachesFallback(t *testing.T) {
	r := mustNew(t,
		WithLookup(lookupOf("known.tld", handlerWriting("custom"))),
		WithFallback(handlerWriting("fallback")),
	)
	assert.Equal(t, "fallback", serve(r, "unknown.tld").Body.String())
}

func TestLookup_NilHandlerWithNoErrorReachesFallback(t *testing.T) {
	r := mustNew(t,
		WithLookup(func(context.Context, string) (http.Handler, error) { return nil, nil }),
		WithFallback(handlerWriting("fallback")),
	)
	assert.Equal(t, "fallback", serve(r, "unknown.tld").Body.String())
}

func TestLookup_FailsClosedOnStoreError(t *testing.T) {
	r := mustNew(t,
		WithLookup(func(context.Context, string) (http.Handler, error) { return nil, errStoreDown }),
		WithFallback(handlerWriting("fallback")),
	)

	rec := serve(r, "shop.customer.tld")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "store down")
	assert.NotContains(t, rec.Body.String(), "fallback")
}

func TestLookup_ErrorHandlerReceivesTheCause(t *testing.T) {
	var got error
	r := mustNew(t,
		WithLookup(func(context.Context, string) (http.Handler, error) { return nil, errStoreDown }),
		WithLookupErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error) {
			got = err
			w.WriteHeader(http.StatusBadGateway)
		}),
	)

	rec := serve(r, "shop.customer.tld")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	require.Error(t, got)
	assert.ErrorIs(t, got, errStoreDown)
}

func TestLookup_SkippedForEmptyHost(t *testing.T) {
	called := false
	r := mustNew(t,
		WithLookup(func(context.Context, string) (http.Handler, error) {
			called = true
			return handlerWriting("lookup"), nil
		}),
		WithFallback(handlerWriting("fallback")),
	)

	assert.Equal(t, "fallback", serve(r, "").Body.String())
	assert.False(t, called)
}

func TestLookup_MatchCarriesHostWithoutPattern(t *testing.T) {
	var got Match
	var ok bool
	r := mustNew(t, WithLookup(lookupOf("shop.customer.tld", captureMatch(&got, &ok))))

	serve(r, "shop.customer.tld")
	require.True(t, ok)
	assert.Equal(t, "shop.customer.tld", got.Host)
	assert.Empty(t, got.Pattern)
	assert.Empty(t, got.Subdomain)
}

func TestLookup_ReceivesRequestContext(t *testing.T) {
	type ctxKey struct{}
	var seen any
	r := mustNew(t, WithLookup(func(ctx context.Context, _ string) (http.Handler, error) {
		seen = ctx.Value(ctxKey{})
		return nil, ErrHostNotFound
	}))

	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "shop.customer.tld"
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "carried"))
	r.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "carried", seen)
}
