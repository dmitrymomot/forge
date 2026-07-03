package httpserver_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/httpserver"
)

func TestOptions_LastWins(t *testing.T) {
	// WithConfig applied last replaces the whole block, so Addr resolves to ":1".
	got := httpserver.New(noopHandler(),
		httpserver.WithAddr(":9090"),
		httpserver.WithConfig(httpserver.Config{Addr: ":1", ReadTimeout: 1}),
	).Name()
	assert.Equal(t, "http :1", got)

	// WithAddr applied last wins over an earlier WithConfig.
	got = httpserver.New(noopHandler(),
		httpserver.WithConfig(httpserver.Config{Addr: ":1"}),
		httpserver.WithAddr(":9090"),
	).Name()
	assert.Equal(t, "http :9090", got)
}

func TestRun_NilOptionRejected(t *testing.T) {
	opts := map[string]httpserver.Option{
		"listener":    httpserver.WithListener(nil),
		"tlsconfig":   httpserver.WithTLSConfig(nil),
		"basecontext": httpserver.WithBaseContext(nil),
		"connstate":   httpserver.WithConnState(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			// A real handler is passed so the failure is unambiguously the option's
			// rejection (ErrInvalidConfig), not ErrNoHandler. Validation runs before
			// any net.Listen, so there is no I/O and Run returns immediately.
			err := httpserver.New(noopHandler(), opt).Run(t.Context())
			require.Error(t, err)
			assert.ErrorIs(t, err, httpserver.ErrInvalidConfig)
		})
	}
}

func TestRun_WithLoggerNilAllowed(t *testing.T) {
	// The listener is pre-bound by startServed, so Run reaches its serve/drain path
	// even with an immediate cancel; the assertion is simply that a nil logger is
	// accepted and Run returns a clean nil. No sleep needed.
	_, done, cancel := startServed(t, noopHandler(), httpserver.WithLogger(nil))
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "a nil logger is allowed, not a validation error")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_ConnStateCallbackFires(t *testing.T) {
	states := make(chan http.ConnState, 8)
	cb := func(_ net.Conn, s http.ConnState) {
		select {
		case states <- s: // buffered, non-blocking so the server goroutine never blocks
		default:
		}
	}
	url, _, cancel := startServed(t, noopHandler(), httpserver.WithConnState(cb))

	var ok bool
	for range 50 {
		resp, err := http.Get(url)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, ok, "server did not become ready")
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-states:
			if s == http.StateNew {
				return // the callback was wired and fired
			}
		case <-deadline:
			t.Fatal("ConnState callback never reported http.StateNew")
		}
	}
}

func TestRun_WithBaseContext_IsUsed(t *testing.T) {
	type ctxKey struct{}
	got := make(chan any, 1)
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case got <- r.Context().Value(ctxKey{}): // buffered, non-blocking
		default:
		}
	})
	base := func() context.Context {
		return context.WithValue(context.Background(), ctxKey{}, "base-ctx-value")
	}
	url, _, cancel := startServed(t, h, httpserver.WithBaseContext(base))

	var ok bool
	for range 50 {
		resp, err := http.Get(url)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, ok, "server did not become ready")
	cancel()

	select {
	case v := <-got:
		assert.Equal(t, "base-ctx-value", v, "the WithBaseContext factory's context must reach request handlers")
	case <-time.After(2 * time.Second):
		t.Fatal("handler never observed the base-context value")
	}
}
