package httpserver_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/httpserver"
)

func noopHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func TestName_Derivation(t *testing.T) {
	// Also covers the seeded default Addr: New(handler).Name() derives "http :8080".
	assert.Equal(t, "http :8080", httpserver.New(noopHandler()).Name())
	assert.Equal(t, "api", httpserver.New(noopHandler(), httpserver.WithName("api")).Name())
	assert.Equal(t, "http :9090", httpserver.New(noopHandler(), httpserver.WithAddr(":9090")).Name())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	s := httpserver.New(noopHandler(), httpserver.WithListener(ln))
	assert.Equal(t, "http "+ln.Addr().String(), s.Name())
}

func TestAddr_BeforeRun(t *testing.T) {
	assert.Equal(t, ":8080", httpserver.New(noopHandler()).Addr())
	assert.Equal(t, ":9090", httpserver.New(noopHandler(), httpserver.WithAddr(":9090")).Addr())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	s := httpserver.New(noopHandler(), httpserver.WithListener(ln))
	assert.Equal(t, ln.Addr().String(), s.Addr())
}

// TestAddr_ReportsTheKernelChosenPort is the reason Addr exists: with ":0" the port
// is unknown until the listener binds, so a caller cannot build a URL from Config.
func TestAddr_ReportsTheKernelChosenPort(t *testing.T) {
	s := httpserver.New(noopHandler(), httpserver.WithAddr("127.0.0.1:0"))
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		return s.Addr() != "127.0.0.1:0"
	}, time.Second, 5*time.Millisecond)

	host, port, err := net.SplitHostPort(s.Addr())
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", host)
	assert.NotEqual(t, "0", port)

	resp, err := http.Get("http://" + s.Addr())
	require.NoError(t, err)
	_ = resp.Body.Close()

	cancel()
	require.NoError(t, <-done)
}

// startServed runs s.Run in a goroutine on a 127.0.0.1:0 listener and returns the
// bound base URL, the channel carrying Run's result, and a cancel func. cancel is
// also registered with t.Cleanup so a failing test never leaks the server goroutine.
func startServed(t *testing.T, h http.Handler, opts ...httpserver.Option) (string, <-chan error, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := httpserver.New(h, append(opts, httpserver.WithListener(ln))...)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return "http://" + ln.Addr().String(), done, cancel
}

func TestRun_RoundTripAndGracefulStop(t *testing.T) {
	url, done, cancel := startServed(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

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
		close(started)
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
	err := httpserver.New(nil).Run(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, httpserver.ErrNoHandler)
}

func TestRun_InvalidConfigReturnsError(t *testing.T) {
	err := httpserver.New(noopHandler(), httpserver.WithConfig(httpserver.Config{Addr: ""})).Run(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, httpserver.ErrInvalidConfig)
}

func TestRun_BindFailureReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	// Reuse the same address without WithListener so net.Listen fails (in use).
	s := httpserver.New(noopHandler(), httpserver.WithAddr(ln.Addr().String()))
	err = s.Run(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, httpserver.ErrShutdownTimeout)
}

func TestRun_ForceCloseOnSlowHandler(t *testing.T) {
	handlerCtxDone := make(chan struct{}, 1)
	cfg := httpserver.DefaultConfig()
	cfg.ShutdownTimeout = 50 * time.Millisecond
	url, done, cancel := startServed(t,
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done() // blocks until force-close cancels the base context
			handlerCtxDone <- struct{}{}
		}),
		httpserver.WithConfig(cfg),
	)

	go func() {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		_, _ = http.DefaultClient.Do(req) // connection drops on force-close
	}()
	time.Sleep(100 * time.Millisecond) // let the request reach the handler

	cancel()
	select {
	case runErr := <-done:
		assert.ErrorIs(t, runErr, httpserver.ErrShutdownTimeout)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after force-close")
	}
	select {
	case <-handlerCtxDone:
	case <-time.After(time.Second):
		t.Fatal("handler base context was not cancelled at force-close")
	}
}

// errBoomListener is a fake net.Listener whose Accept always fails with a permanent
// error, so http.Server.Serve returns that error into Run's serve channel.
// accepted is signaled (receives a token) on the first Accept call so the test can
// synchronize before firing cancel — proving the serve goroutine has entered Accept.
type errBoomListener struct {
	err      error
	accepted chan struct{}
}

func (l errBoomListener) Accept() (net.Conn, error) {
	select {
	case l.accepted <- struct{}{}:
	default:
	}
	return nil, l.err
}
func (errBoomListener) Close() error   { return nil }
func (errBoomListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// TestRun_SurfacesServeErrorRacingCancel is the black-box replacement for the former
// white-box drain test. It proves the documented contract: "a serve error that races
// with cancellation is always surfaced, never masked as a clean stop." The fake
// listener fails Accept immediately, so Serve buffers the error before shutdown can
// substitute http.ErrServerClosed; Run then surfaces it whichever select branch wins
// (direct serve-error read, or the drain path's buffered read).
func TestRun_SurfacesServeErrorRacingCancel(t *testing.T) {
	boom := errors.New("boom")
	accepted := make(chan struct{}, 1)
	s := httpserver.New(noopHandler(), httpserver.WithListener(errBoomListener{err: boom, accepted: accepted}))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Accept signals before returning the error, so by the time we cancel, the serve
	// goroutine has at least entered Accept; whichever branch Run takes, the buffered
	// serve error (boom) is surfaced — directly via the serve-error read, or by drain,
	// which reads serveErr and returns any non-ErrServerClosed error.
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("Accept was never called")
	}
	cancel() // race cancellation against the serve failure

	select {
	case err := <-done:
		require.ErrorIs(t, err, boom, "a serve error racing cancellation must always be surfaced")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}
