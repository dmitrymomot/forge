package httpclient_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/httpclient"
)

func TestNew_RetriesTransient503ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := httpclient.New()
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), calls.Load())
}

func TestNew_DoesNotRetryPOSTByDefault(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := httpclient.New()
	resp, err := client.Post(srv.URL, "text/plain", nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, int32(1), calls.Load()) // POST not retried
}

func TestNew_PropagatesContextHeaders(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := httpclient.New(
		httpclient.WithContextHeaders(func(ctx context.Context) http.Header {
			h := http.Header{}
			if v, ok := ctx.Value(ctxKey{}).(string); ok {
				h.Set("X-Request-ID", v)
			}
			return h
		}),
	)
	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), ctxKey{}, "rid-1"), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "rid-1", got)
}

type ctxKey struct{}

// trackedBody wraps a response body and records whether Close was called, so
// tests can prove a superseded (retried-past) response was released.
type trackedBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackedBody) Close() error {
	b.closed.Store(true)
	return nil
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNew_ClosesSupersededRetryResponseBody(t *testing.T) {
	firstBody := &trackedBody{Reader: bytes.NewBufferString("first")}
	secondBody := &trackedBody{Reader: bytes.NewBufferString("second")}

	var calls atomic.Int32
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       firstBody,
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       secondBody,
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := httpclient.New(httpclient.WithBaseTransport(base))
	resp, err := client.Get("http://example.invalid/")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), calls.Load())
	assert.True(t, firstBody.closed.Load(), "superseded 503 response body must be drained and closed")
	assert.False(t, secondBody.closed.Load(), "final 200 response body must remain open for the caller")
}
