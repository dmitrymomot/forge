package debug_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/debug"
)

func TestServerName(t *testing.T) {
	t.Parallel()
	if got := debug.NewServer().Name(); got != "debug" {
		t.Fatalf("Name() = %q, want debug", got)
	}
	cfg := debug.DefaultConfig()
	cfg.Name = "diagnostics"
	if got := debug.NewServer(debug.WithConfig(cfg)).Name(); got != "diagnostics" {
		t.Fatalf("Name() = %q, want diagnostics", got)
	}
}

func TestServerRunFailsClosedOnNonLoopback(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{":6060", "0.0.0.0:6060", "example.internal:6060", "not-an-addr"} {
		srv := debug.NewServer(debug.WithAddr(addr))
		err := srv.Run(context.Background())
		if !errors.Is(err, debug.ErrAuthRequired) {
			t.Errorf("Run(addr=%q) = %v, want ErrAuthRequired", addr, err)
		}
	}
}

func TestServerRunNonLoopbackListenerFailsClosed(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	srv := debug.NewServer(debug.WithListener(ln))
	if err := srv.Run(context.Background()); !errors.Is(err, debug.ErrAuthRequired) {
		t.Fatalf("Run() = %v, want ErrAuthRequired", err)
	}
}

func TestServerRunInvalidConfig(t *testing.T) {
	t.Parallel()
	srv := debug.NewServer(debug.WithConfig(debug.Config{Addr: "", ReadHeaderTimeout: -1}))
	err := srv.Run(context.Background())
	if !errors.Is(err, debug.ErrInvalidConfig) {
		t.Fatalf("Run() = %v, want ErrInvalidConfig", err)
	}
}

func TestServerRunNilListenerRejected(t *testing.T) {
	t.Parallel()
	srv := debug.NewServer(debug.WithListener(nil))
	if err := srv.Run(context.Background()); !errors.Is(err, debug.ErrInvalidConfig) {
		t.Fatalf("Run() = %v, want ErrInvalidConfig", err)
	}
}

// runServer starts srv, waits until it accepts requests at base, and returns a
// stop func that cancels Run and reports its error.
func runServer(t *testing.T, srv *debug.Server) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	return func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
			return nil
		}
	}
}

// drain reads the body to EOF and closes it, so the server-side connection
// returns to idle and Shutdown can close it instead of waiting out the timeout.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func waitReady(t *testing.T, url string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:gosec // test URL from local listener
		if err == nil && resp != nil {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerServesOnLoopbackWithoutAuth(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := debug.NewServer(debug.WithListener(ln))
	stop := runServer(t, srv)

	resp := waitReady(t, fmt.Sprintf("http://%s/debug/stats", ln.Addr()))
	drain(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
}

func TestServerBasicAuthEndToEnd(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := debug.NewServer(
		debug.WithListener(ln),
		debug.WithBasicAuth(map[string]string{"ops": "s3cret"}),
	)
	stop := runServer(t, srv)

	url := fmt.Sprintf("http://%s/debug/stats", ln.Addr())
	resp := waitReady(t, url)
	drain(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", resp.StatusCode)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("ops", "s3cret")
	authed, err := http.DefaultClient.Do(req)
	if err != nil || authed == nil {
		t.Fatalf("authenticated request: %v", err)
	}
	drain(authed)
	if authed.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", authed.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
}

func TestServerWithoutAuthAllowsNonLoopback(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := debug.NewServer(debug.WithListener(ln), debug.WithoutAuth())
	stop := runServer(t, srv)
	resp := waitReady(t, fmt.Sprintf("http://127.0.0.1:%d/debug/stats", ln.Addr().(*net.TCPAddr).Port))
	drain(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	if err := debug.DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() = %v, want nil", err)
	}
	bad := debug.Config{Addr: "", ReadHeaderTimeout: -1, ShutdownTimeout: -1}
	err := bad.Validate()
	if !errors.Is(err, debug.ErrInvalidConfig) {
		t.Fatalf("Validate() = %v, want ErrInvalidConfig", err)
	}
}
