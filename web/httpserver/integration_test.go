package httpserver_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/supervisor"
	"github.com/dmitrymomot/forge/web/httpserver"
)

// TestServer_RunsUnderSupervisor proves *httpserver.Server satisfies
// supervisor.Service (the WithService call compiles only if it does) and that the
// supervisor drives its lifecycle: serve, then coordinated graceful stop on cancel.
func TestServer_RunsUnderSupervisor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := httpserver.New(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		httpserver.WithListener(ln),
		httpserver.WithName("api"),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx, supervisor.WithService(srv)) }()

	url := "http://" + ln.Addr().String()
	var ok bool
	for range 50 {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, ok, "server did not become ready under supervisor")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor.Run did not return after cancel")
	}
}
