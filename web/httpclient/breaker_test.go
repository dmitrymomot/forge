package httpclient_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
	"github.com/dmitrymomot/forge/web/httpclient"
)

func TestBreaker_OptInTripsAfterFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := httpclient.New(
		httpclient.WithRetryMethods(), // disable retry to isolate the breaker
		httpclient.WithBreakerGroup(circuitbreaker.WithBreakerOptions(circuitbreaker.WithFailureThreshold(2))),
	)
	// Drive failures past the threshold.
	for range 3 {
		resp, err := client.Get(srv.URL)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}
	// Next call should fast-fail with ErrOpen.
	_, err := client.Get(srv.URL)
	require.Error(t, err)
	assert.True(t, errors.Is(err, circuitbreaker.ErrOpen))
}

func TestBreaker_OffByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := httpclient.New(httpclient.WithRetryMethods())
	for range 10 {
		resp, err := client.Get(srv.URL)
		require.NoError(t, err) // 500 is a response, not an error; never ErrOpen
		require.NotNil(t, resp)
		_ = resp.Body.Close()
	}
}
