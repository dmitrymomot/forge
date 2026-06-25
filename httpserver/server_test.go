package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func TestNew_SeedsDefaults(t *testing.T) {
	s := New(noopHandler())
	assert.Equal(t, ":8080", s.cfg.Addr)
	assert.Equal(t, 1<<20, s.cfg.MaxHeaderBytes)
	assert.NotNil(t, s.cfg.handler)
}

func TestName_Derivation(t *testing.T) {
	assert.Equal(t, "http :8080", New(noopHandler()).Name())
	assert.Equal(t, "api", New(noopHandler(), WithName("api")).Name())
	assert.Equal(t, "http :9090", New(noopHandler(), WithAddr(":9090")).Name())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	s := New(noopHandler(), WithListener(ln))
	assert.Equal(t, "http "+ln.Addr().String(), s.Name())
}

// startServed runs s.Run in a goroutine on a 127.0.0.1:0 listener and returns the
// bound base URL, the channel carrying Run's result, and a cancel func. cancel is
// also registered with t.Cleanup so a failing test never leaks the server goroutine.
func startServed(t *testing.T, h http.Handler, opts ...Option) (string, <-chan error, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := New(h, append(opts, WithListener(ln))...)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return "http://" + ln.Addr().String(), done, cancel
}

func TestRun_RoundTripAndGracefulStop(t *testing.T) {
	url, done, cancel := startServed(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	// Retry until the server accepts; assert inside the guard so resp is non-nil
	// (keeps nilaway happy without a //nolint).
	var ok bool
	for range 50 {
		resp, err := http.Get(url)
		if err == nil && resp != nil {
			assert.Equal(t, http.StatusTeapot, resp.StatusCode)
			_ = resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, ok, "server did not become ready")

	cancel()
	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_GracefulDrainCompletesInflight(t *testing.T) {
	started := make(chan struct{})
	url, done, cancel := startServed(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)                     // exactly one request is fired in this test
		time.Sleep(100 * time.Millisecond) // still serving when ctx is cancelled
		w.WriteHeader(http.StatusNoContent)
	}))

	codeCh := make(chan int, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil || resp == nil {
			codeCh <- -1
			return
		}
		_ = resp.Body.Close()
		codeCh <- resp.StatusCode
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}
	cancel() // begin shutdown while the request is in-flight (default 15s drain)

	select {
	case code := <-codeCh:
		assert.Equal(t, http.StatusNoContent, code, "in-flight request must finish during graceful drain")
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not complete")
	}
	require.NoError(t, <-done)
}

func TestRun_NilHandlerReturnsErrNoHandler(t *testing.T) {
	err := New(nil).Run(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHandler)
}

func TestRun_InvalidConfigReturnsError(t *testing.T) {
	err := New(noopHandler(), WithConfig(Config{Addr: ""})).Run(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRun_BindFailureReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	// Reuse the same address without WithListener so net.Listen fails (in use).
	s := New(noopHandler(), WithAddr(ln.Addr().String()))
	err = s.Run(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrShutdownTimeout)
}

func TestRun_ForceCloseOnSlowHandler(t *testing.T) {
	handlerCtxDone := make(chan struct{}, 1)
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = 50 * time.Millisecond
	url, done, cancel := startServed(t,
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done() // blocks until force-close cancels the base context
			handlerCtxDone <- struct{}{}
		}),
		WithConfig(cfg),
	)

	// Fire a request that will be in-flight when we cancel.
	go func() {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		_, _ = http.DefaultClient.Do(req) // connection drops on force-close
	}()
	time.Sleep(100 * time.Millisecond) // let the request reach the handler

	cancel()
	select {
	case runErr := <-done:
		assert.ErrorIs(t, runErr, ErrShutdownTimeout)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after force-close")
	}
	select {
	case <-handlerCtxDone:
	case <-time.After(time.Second):
		t.Fatal("handler base context was not cancelled at force-close")
	}
}

// TestDrain_SurfacesBufferedServeError deterministically covers the lost-error
// race: a serve error already buffered when shutdown begins must be returned by
// drain, never masked as a clean nil. drain is called directly with a pre-seeded
// channel and a fresh *http.Server (whose Shutdown returns nil immediately).
func TestDrain_SurfacesBufferedServeError(t *testing.T) {
	s := New(noopHandler())
	boom := errors.New("boom")
	serveErr := make(chan error, 1)
	serveErr <- boom

	err := s.drain(&http.Server{}, serveErr, func() {}, resolveLogger(nil))
	require.ErrorIs(t, err, boom)
}
