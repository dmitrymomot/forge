# Black-Box Test Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Convert every `*_test.go` file in `httpserver/` and `supervisor/` to black-box tests (`package httpserver_test` / `package supervisor_test`) that exercise only the exported API and observable behavior, with zero white-box test files.

**Architecture:** Production code is **not touched**. Each task rewrites one test file (or a small compile-coupled cluster) to the external `_test` package, re-expressing every white-box assertion as an observable one: sentinel-error identity via `errors.Is`, HTTP round-trips, structured-log records, and (for the two formerly-unreachable cases) a fake `net.Listener` and a subprocess re-exec. Tasks are ordered so the package compiles and all tests stay green at every commit, even while some files are still internal-package and others have flipped (Go allows `pkg` and `pkg_test` test files to coexist in one directory).

**Tech Stack:** Go 1.26, `testify` (`assert`/`require`) in tests only; stdlib `net/http`, `net`, `context`, `log/slog`, `os`, `os/exec`, `encoding/json`, `bytes`, `reflect`.

**Spec:** `docs/superpowers/specs/2026-06-26-blackbox-tests-design.md`

## Global Constraints

- **Module:** `github.com/dmitrymomot/forge`. Import paths: `github.com/dmitrymomot/forge/httpserver`, `github.com/dmitrymomot/forge/supervisor`.
- **No production-code changes.** Only `*_test.go` files under `httpserver/` and `supervisor/` change. `git diff` must show no non-test `.go` file modified.
- **No new dependencies.** `testify` is already present; add nothing.
- **Every test file is black-box:** `package httpserver_test` / `package supervisor_test`, importing the package; assert only via exported identifiers + observable behavior. Zero white-box files remain.
- **Lint must stay green.** `just lint` runs `go vet`, `go build`, `golangci-lint`, `nilaway`, `betteralign`. The local import (`github.com/dmitrymomot/forge/...`) must sit in its own goimports group, and any new test struct must be betteralign-ordered — `just fmt` fixes both automatically, so **run `just fmt` after editing any file**.
- **nilaway:** carry `//nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above` on any `err.Error()` call that follows `require.ErrorIs`/`require.Error`. Guard every `*http.Response` use behind `err == nil && resp != nil`.
- **Use `http.StateNew`** (not `http.ConnStateNew`, which does not exist).
- **One shared test package per directory:** every helper/type is declared exactly once across the package — `noopHandler`, `startServed`, `errBoomListener` in httpserver's test package; `fakeService` (in `helpers_test.go`) and `discardLogger` (in `supervisor_test.go`) shared across supervisor's test files.
- **Verify with `just`:** per-task `just test ./httpserver/` or `just test ./supervisor/` (`go clean -testcache && go test -race -cover`); the final task runs full `just check` (`fmt` + `lint` + `test`).
- **Commit after every task.** Commit on the current worktree branch.

## TDD note for this refactor

This is a **test migration against unchanged production code**, so the usual red→green order is inverted: the safety property is that the rewritten tests **stay green** (behavior preserved) and lint **stays clean**. Each task therefore is: rewrite the file → run `just test ./<pkg>/` and confirm **PASS** → `just fmt` + `just lint` clean → commit. The two new tests (fake-listener drain replacement, subprocess recover) assert behavior that already exists, so they also pass immediately; each task notes what a genuine failure would look like.

## Task ordering (compile-safe at every commit)

httpserver: **H1** config → **H2** tls → **H3** server → **H4** options.
supervisor: **S1** config → **S2** context → **S3** helpers+supervisor (delete options) → **S4** service.
Finally **T9** full-suite verification. httpserver and supervisor are independent; H* and S* may interleave.

Why this order: `httpserver/options_test.go` (reframed) needs `noopHandler`/`startServed`, which live in `server_test.go` — so server flips first (H3), then options (H4). In supervisor, `supervisor_test.go` and `options_test.go` both use the `fakeService` helper, so `helpers_test.go` must flip together with `supervisor_test.go` while `options_test.go` is deleted in the same commit (S3); `service_test.go` uses neither helper and stays internal until S4.

---

### Task H1: httpserver/config_test.go → black-box (trivial swap)

**Files:**
- Modify: `httpserver/config_test.go` (whole file)

**Interfaces:**
- Consumes: nothing.
- Produces: no shared helpers.

- [ ] **Step 1: Rewrite the file** — replace the entire contents of `httpserver/config_test.go` with:

```go
package httpserver_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/httpserver"
)

func TestDefaultConfig(t *testing.T) {
	cfg := httpserver.DefaultConfig()
	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, 15*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 10*time.Second, cfg.ReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout)
	assert.Equal(t, 1<<20, cfg.MaxHeaderBytes)
	assert.Empty(t, cfg.Name, "Name defaults empty so Name() derives it")
	assert.Empty(t, cfg.TLSCertFile)
	assert.Empty(t, cfg.TLSKeyFile)
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	tests := map[string]httpserver.Config{
		"empty addr":      {Addr: ""},
		"neg shutdown":    {Addr: ":0", ShutdownTimeout: -1},
		"neg read header": {Addr: ":0", ReadHeaderTimeout: -1},
		"neg read":        {Addr: ":0", ReadTimeout: -1},
		"neg write":       {Addr: ":0", WriteTimeout: -1},
		"neg idle":        {Addr: ":0", IdleTimeout: -1},
		"neg maxheader":   {Addr: ":0", MaxHeaderBytes: -1},
		"half tls (cert)": {Addr: ":0", TLSCertFile: "c.pem"},
		"half tls (key)":  {Addr: ":0", TLSKeyFile: "k.pem"},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, httpserver.ErrInvalidConfig)
		})
	}

	require.NoError(t, httpserver.Config{Addr: ":0", TLSCertFile: "c.pem", TLSKeyFile: "k.pem"}.Validate())
	require.NoError(t, httpserver.Config{Addr: ":0", WriteTimeout: 0}.Validate())
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"Addr":              "ADDR",
		"Name":              "NAME",
		"ShutdownTimeout":   "SHUTDOWN_TIMEOUT",
		"ReadHeaderTimeout": "READ_HEADER_TIMEOUT",
		"ReadTimeout":       "READ_TIMEOUT",
		"WriteTimeout":      "WRITE_TIMEOUT",
		"IdleTimeout":       "IDLE_TIMEOUT",
		"MaxHeaderBytes":    "MAX_HEADER_BYTES",
		"TLSCertFile":       "TLS_CERT_FILE",
		"TLSKeyFile":        "TLS_KEY_FILE",
	}
	typ := reflect.TypeFor[httpserver.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}
```

- [ ] **Step 2: Format** — Run: `just fmt`
- [ ] **Step 3: Run tests** — Run: `just test ./httpserver/`
  Expected: PASS (all httpserver tests green; `config_test.go` now compiles as `httpserver_test` alongside the still-internal files).
- [ ] **Step 4: Lint** — Run: `just lint`
  Expected: clean (0 issues).
- [ ] **Step 5: Commit**

```bash
git add httpserver/config_test.go
git commit -m "test(httpserver): make config_test black-box"
```

---

### Task H2: httpserver/tls_test.go → black-box (trivial swap)

**Files:**
- Modify: `httpserver/tls_test.go` (whole file)

**Interfaces:**
- Consumes: nothing (self-contained helpers).
- Produces: `selfSigned`, `tlsClient`, `startTLS`, `waitTLS200` (TLS-only helpers; not used by other files).

- [ ] **Step 1: Rewrite the file** — replace the entire contents of `httpserver/tls_test.go` with:

```go
package httpserver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/httpserver"
)

// selfSigned returns an in-memory cert plus its PEM bytes for the loopback host.
func selfSigned(t *testing.T) (tls.Certificate, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return pair, certPEM, keyPEM
}

// tlsClient trusts exactly the supplied CA/cert PEM — no InsecureSkipVerify.
func tlsClient(t *testing.T, caPEM []byte) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
}

func startTLS(t *testing.T, opts ...httpserver.Option) (string, <-chan error, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	s := httpserver.New(h, append(opts, httpserver.WithListener(ln))...)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return "https://" + ln.Addr().String(), done, cancel
}

func waitTLS200(t *testing.T, url string, caPEM []byte) {
	t.Helper()
	c := tlsClient(t, caPEM)
	for range 50 {
		resp, err := c.Get(url)
		if err == nil && resp != nil {
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			_ = resp.Body.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("TLS server never served 200")
}

func TestRun_TLSWithConfig(t *testing.T) {
	cert, certPEM, _ := selfSigned(t)
	url, done, cancel := startTLS(t, httpserver.WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}))
	waitTLS200(t, url, certPEM)
	cancel()
	require.NoError(t, <-done)
}

func TestRun_TLSWithCertFiles(t *testing.T) {
	_, certPEM, keyPEM := selfSigned(t)
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))

	cfg := httpserver.DefaultConfig()
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	url, done, cancel := startTLS(t, httpserver.WithConfig(cfg))
	waitTLS200(t, url, certPEM)
	cancel()
	require.NoError(t, <-done)
}

func TestRun_TLSConfigTakesPrecedenceOverFiles(t *testing.T) {
	cert, certPEM, _ := selfSigned(t)
	// Bogus cert files that would fail to load IF they were used. WithTLSConfig must win.
	cfg := httpserver.DefaultConfig()
	cfg.TLSCertFile = "/nonexistent/cert.pem"
	cfg.TLSKeyFile = "/nonexistent/key.pem"
	url, done, cancel := startTLS(t,
		httpserver.WithConfig(cfg),
		httpserver.WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}),
	)
	waitTLS200(t, url, certPEM) // would fail if the bogus files were loaded
	cancel()
	require.NoError(t, <-done)
}
```

- [ ] **Step 2: Format** — Run: `just fmt`
- [ ] **Step 3: Run tests** — Run: `just test ./httpserver/`
  Expected: PASS.
- [ ] **Step 4: Lint** — Run: `just lint`
  Expected: clean.
- [ ] **Step 5: Commit**

```bash
git add httpserver/tls_test.go
git commit -m "test(httpserver): make tls_test black-box"
```

---

### Task H3: httpserver/server_test.go → black-box (swaps + drain replacement)

**Files:**
- Modify: `httpserver/server_test.go` (whole file)

**Interfaces:**
- Consumes: nothing.
- Produces (shared with H4 via the `httpserver_test` package): `func noopHandler() http.Handler`, `func startServed(t *testing.T, h http.Handler, opts ...httpserver.Option) (string, <-chan error, context.CancelFunc)`, `type errBoomListener`.

Note: after this task, `server_test.go` is `package httpserver_test` while `options_test.go` is still `package httpserver` (it uses its own internal `baseConfig` helper, so it keeps compiling). They share no helpers, so the mixed state is valid.

- [ ] **Step 1: Rewrite the file** — replace the entire contents of `httpserver/server_test.go` with the following. Note three deliberate changes from the original: (a) `TestNew_SeedsDefaults` is **removed** — its only observable assertion (`New(noop).Name() == "http :8080"`) already lives in `TestName_Derivation`; (b) `TestDrain_SurfacesBufferedServeError` (which called the unexported `drain`) is **replaced** by the black-box `TestRun_SurfacesServeErrorRacingCancel`; (c) the `errBoomListener` fake listener is added.

```go
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

	"github.com/dmitrymomot/forge/httpserver"
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
type errBoomListener struct{ err error }

func (l errBoomListener) Accept() (net.Conn, error) { return nil, l.err }
func (errBoomListener) Close() error                { return nil }
func (errBoomListener) Addr() net.Addr              { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// TestRun_SurfacesServeErrorRacingCancel is the black-box replacement for the former
// white-box drain test. It proves the documented contract: "a serve error that races
// with cancellation is always surfaced, never masked as a clean stop." The fake
// listener fails Accept immediately, so Serve buffers the error before shutdown can
// substitute http.ErrServerClosed; Run then surfaces it whichever select branch wins
// (direct serve-error read, or the drain path's buffered read). Verified 3000/3000.
func TestRun_SurfacesServeErrorRacingCancel(t *testing.T) {
	boom := errors.New("boom")
	s := httpserver.New(noopHandler(), httpserver.WithListener(errBoomListener{err: boom}))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	cancel() // race cancellation against the serve failure

	select {
	case err := <-done:
		require.ErrorIs(t, err, boom, "a serve error racing cancellation must always be surfaced")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}
```

- [ ] **Step 2: Format** — Run: `just fmt`
- [ ] **Step 3: Run tests** — Run: `just test ./httpserver/`
  Expected: PASS. (To confirm `TestRun_SurfacesServeErrorRacingCancel` is not a no-op, note that it would FAIL if `Run`'s drain ever returned `nil` while a real serve error was buffered — the exact regression the original white-box test guarded.)
- [ ] **Step 4: Lint** — Run: `just lint`
  Expected: clean.
- [ ] **Step 5: Commit**

```bash
git add httpserver/server_test.go
git commit -m "test(httpserver): make server_test black-box; replace drain test with fake-listener race test"
```

---

### Task H4: httpserver/options_test.go → black-box (behavioral reframe)

**Files:**
- Modify: `httpserver/options_test.go` (whole file)

**Interfaces:**
- Consumes (from H3, same `httpserver_test` package): `noopHandler`, `startServed`.
- Produces: no shared helpers.

This rewrite deletes the internal `baseConfig()` helper and re-expresses every option assertion behaviorally. The `WithConnState` wiring coverage formerly in `TestCodeOptions_StoreNonNil` is preserved by the new `TestRun_ConnStateCallbackFires`.

- [ ] **Step 1: Rewrite the file** — replace the entire contents of `httpserver/options_test.go` with:

```go
package httpserver_test

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/httpserver"
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
	_, done, cancel := startServed(t, noopHandler(), httpserver.WithLogger(nil))
	time.Sleep(20 * time.Millisecond)
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
```

- [ ] **Step 2: Format** — Run: `just fmt`
- [ ] **Step 3: Run tests** — Run: `just test ./httpserver/`
  Expected: PASS. All `httpserver` test files are now `package httpserver_test`.
- [ ] **Step 4: Verify the flip** — Run: `grep -L "package httpserver_test" httpserver/*_test.go`
  Expected: no output (every test file is now `httpserver_test`).
- [ ] **Step 5: Lint** — Run: `just lint`
  Expected: clean.
- [ ] **Step 6: Commit**

```bash
git add httpserver/options_test.go
git commit -m "test(httpserver): make options_test black-box; add ConnState wiring test"
```

---

### Task S1: supervisor/config_test.go → black-box (trivial swap)

**Files:**
- Modify: `supervisor/config_test.go` (whole file)

**Interfaces:**
- Consumes: nothing.
- Produces: no shared helpers.

- [ ] **Step 1: Rewrite the file** — replace the entire contents of `supervisor/config_test.go` with:

```go
package supervisor_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/supervisor"
)

func TestExportedDefaultConfig(t *testing.T) {
	cfg := supervisor.DefaultConfig()
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.True(t, cfg.Recover)
}

func TestConfig_Validate(t *testing.T) {
	require.NoError(t, supervisor.DefaultConfig().Validate())
	require.NoError(t, supervisor.Config{ShutdownTimeout: 0, Recover: false}.Validate())

	err := supervisor.Config{ShutdownTimeout: -1}.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"ShutdownTimeout": "SHUTDOWN_TIMEOUT",
		"Recover":         "RECOVER",
	}
	typ := reflect.TypeFor[supervisor.Config]()
	for name, tag := range want {
		f, ok := typ.FieldByName(name)
		require.Truef(t, ok, "field %s missing", name)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", name)
	}
}
```

- [ ] **Step 2: Format** — Run: `just fmt`
- [ ] **Step 3: Run tests** — Run: `just test ./supervisor/`
  Expected: PASS.
- [ ] **Step 4: Lint** — Run: `just lint`
  Expected: clean.
- [ ] **Step 5: Commit**

```bash
git add supervisor/config_test.go
git commit -m "test(supervisor): make config_test black-box"
```

---

### Task S2: supervisor/context_test.go → black-box (swap + strengthen)

**Files:**
- Modify: `supervisor/context_test.go` (whole file)

**Interfaces:**
- Consumes: nothing.
- Produces: no shared helpers.

`TestNewContext_StopIsSafe` is strengthened from a bare "no panic" to a positive assertion that `stop()` cancels the context.

- [ ] **Step 1: Rewrite the file** — replace the entire contents of `supervisor/context_test.go` with:

```go
package supervisor_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/supervisor"
)

func TestNewContext_CancelsOnSIGTERM(t *testing.T) {
	ctx, stop := supervisor.NewContext()
	defer stop()

	require.NoError(t, ctx.Err(), "context must be live before any signal")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case <-ctx.Done():
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled after SIGTERM")
	}
}

func TestNewContext_StopIsSafe(t *testing.T) {
	ctx, stop := supervisor.NewContext()
	stop() // releasing the handler must not panic
	assert.ErrorIs(t, ctx.Err(), context.Canceled, "stop cancels the context")
}
```

- [ ] **Step 2: Format** — Run: `just fmt`
- [ ] **Step 3: Run tests** — Run: `just test ./supervisor/`
  Expected: PASS.
- [ ] **Step 4: Lint** — Run: `just lint`
  Expected: clean.
- [ ] **Step 5: Commit**

```bash
git add supervisor/context_test.go
git commit -m "test(supervisor): make context_test black-box"
```

---

### Task S3: supervisor helpers + supervisor_test → black-box; delete options_test

**Files:**
- Modify: `supervisor/helpers_test.go` (whole file — flip package)
- Modify: `supervisor/supervisor_test.go` (whole file — swaps, reframes, subprocess test, absorbed cases)
- Delete: `supervisor/options_test.go`

**Interfaces:**
- Consumes: `fakeService` (from `helpers_test.go`, same package after flip).
- Produces (shared across supervisor test files): `type fakeService` and `func discardLogger() *slog.Logger`.

Why these three move together: `supervisor_test.go` and `options_test.go` both use `fakeService`; once `helpers_test.go` becomes `supervisor_test`, any file still in `package supervisor` that uses `fakeService` would stop compiling. `supervisor_test.go` flips in the same commit, and `options_test.go` (the other `fakeService` user) is deleted — its cases are absorbed below. `service_test.go` uses neither helper, so it stays `package supervisor` until S4.

- [ ] **Step 1: Flip the helper file** — replace the entire contents of `supervisor/helpers_test.go` with:

```go
package supervisor_test

import "context"

// fakeService is a controllable Service test double shared across the supervisor
// black-box test files.
type fakeService struct {
	name string
	run  func(ctx context.Context) error
}

func (f fakeService) Name() string                  { return f.name }
func (f fakeService) Run(ctx context.Context) error { return f.run(ctx) }
```

- [ ] **Step 2: Rewrite supervisor_test.go** — replace the entire contents of `supervisor/supervisor_test.go` with the following. Changes from the original: `TestResolveLogger_PassthroughWhenSet` is **dropped**; `TestResolveLogger_NilReturnsUsableLogger` becomes `TestRun_NilLogger_DoesNotPanic`; the two `TestRunService_RecoverEnabled_*` tests are reframed through `Run`; `TestRunService_RecoverDisabled_Propagates` becomes the subprocess `TestRun_RecoverDisabled_PanicCrashesProcess`; and two cases absorbed from the deleted `options_test.go` are added (`TestRun_NilRegistration_ReturnsErrInvalidConfig`, `TestRun_WithConfigAppliesBlock`).

```go
package supervisor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/supervisor"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRun_NoServices_ReturnsErrNoServices(t *testing.T) {
	err := supervisor.Run(context.Background())
	require.ErrorIs(t, err, supervisor.ErrNoServices)
}

func TestRun_EmptyName_ReturnsErrUnnamedService(t *testing.T) {
	svc := fakeService{name: "", run: func(ctx context.Context) error { return nil }}
	err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))
	require.ErrorIs(t, err, supervisor.ErrUnnamedService)
}

func TestRun_SingleService_ReturnsWrappedError(t *testing.T) {
	sentinel := errors.New("boom")
	svc := fakeService{name: "svc", run: func(ctx context.Context) error { return sentinel }}

	err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), `service "svc"`) //nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above
}

func TestRun_FirstExitStopsAll(t *testing.T) {
	siblingCancelled := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingCancelled)
		return ctx.Err()
	}}
	quick := fakeService{name: "quick", run: func(ctx context.Context) error { return nil }}

	err := supervisor.Run(context.Background(),
		supervisor.WithService(sibling), supervisor.WithService(quick), supervisor.WithLogger(discardLogger()))

	require.NoError(t, err, "quick returns nil; sibling returns context.Canceled which is filtered")
	select {
	case <-siblingCancelled:
	case <-time.After(time.Second):
		t.Fatal("sibling was not cancelled when quick exited")
	}
}

func TestRun_ContextCancel_ShutsDown(t *testing.T) {
	svc := fakeService{name: "svc", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := supervisor.Run(ctx, supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))
	require.NoError(t, err)
}

func TestRun_AggregatesNonCanceledErrors(t *testing.T) {
	errA := errors.New("err-a")
	errB := errors.New("err-b")
	a := fakeService{name: "a", run: func(ctx context.Context) error { return errA }}
	b := fakeService{name: "b", run: func(ctx context.Context) error {
		<-ctx.Done()
		return errB // a real error during drain, NOT context.Canceled
	}}

	err := supervisor.Run(context.Background(), supervisor.WithService(a), supervisor.WithService(b), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, errA)
	require.ErrorIs(t, err, errB)
}

func TestRun_AlreadyCancelledContext_DoesNotStartServices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := false
	svc := fakeService{name: "svc", run: func(ctx context.Context) error {
		started = true
		return nil
	}}

	err := supervisor.Run(ctx, supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))

	require.NoError(t, err)
	assert.False(t, started, "no service may start when ctx is already cancelled")
}

func TestRun_DuplicateNames_Warns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	a := fakeService{name: "dup", run: func(ctx context.Context) error { return nil }}
	b := fakeService{name: "dup", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}

	_ = supervisor.Run(context.Background(), supervisor.WithService(a), supervisor.WithService(b), supervisor.WithLogger(logger))

	assert.Contains(t, buf.String(), "duplicate service name")
}

func TestRun_NilLogger_DoesNotPanic(t *testing.T) {
	svc := fakeService{name: "svc", run: func(ctx context.Context) error { return nil }}
	require.NotPanics(t, func() {
		err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(nil))
		require.NoError(t, err)
	})
}

func TestRun_GraceTimeout_AbandonsStuckService(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stuck := fakeService{name: "stuck", run: func(ctx context.Context) error {
		<-release // deliberately ignores ctx
		return nil
	}}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error {
		return nil // exits immediately -> begins shutdown
	}}

	start := time.Now()
	err := supervisor.Run(context.Background(),
		supervisor.WithService(stuck), supervisor.WithService(trigger),
		supervisor.WithShutdownTimeout(50*time.Millisecond),
		supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrShutdownTimeout)
	assert.Less(t, time.Since(start), 2*time.Second, "must return shortly after the grace timeout")
}

func TestRun_GraceTimeout_LogsStuckNamesStructured(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stuck := fakeService{name: "stuck-svc", run: func(ctx context.Context) error { <-release; return nil }}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error { return nil }}

	_ = supervisor.Run(context.Background(),
		supervisor.WithService(stuck), supervisor.WithService(trigger),
		supervisor.WithShutdownTimeout(30*time.Millisecond), supervisor.WithLogger(logger))

	var timeoutRec map[string]any
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "graceful shutdown timed out" {
			timeoutRec = rec
			break
		}
	}
	require.NotNil(t, timeoutRec, "expected a 'graceful shutdown timed out' log line")
	assert.Contains(t, timeoutRec["stuck"], "stuck-svc", "stuck name must be in the structured 'stuck' attribute")
}

func TestRun_ZeroTimeout_DrainsCooperativeService(t *testing.T) {
	svc := fakeService{name: "coop", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := supervisor.Run(ctx, supervisor.WithService(svc), supervisor.WithShutdownTimeout(0), supervisor.WithLogger(discardLogger()))
	require.NoError(t, err)
}

func TestRun_RecoverEnabled_ReturnsSingleLineErrPanic(t *testing.T) {
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrPanic)
	assert.Contains(t, err.Error(), "kaboom")                                    //nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above
	assert.NotContains(t, err.Error(), "\n", "error string must be single-line") //nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above
}

func TestRun_RecoverEnabled_LogsStackAsAttribute(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	_ = supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(logger))

	var panicRec map[string]any
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "service panicked" {
			panicRec = rec
			break
		}
	}
	require.NotNil(t, panicRec, "expected a 'service panicked' log line")
	assert.Equal(t, "boom", panicRec["service"])
	stack, ok := panicRec["stack"].(string)
	require.True(t, ok, "stack must be a structured string attribute")
	assert.Contains(t, stack, "goroutine")
}

// TestRun_RecoverDisabled_PanicCrashesProcess is the black-box replacement for the
// former white-box recover-disabled test. With recovery off, an unrecovered panic in
// a service goroutine crashes the whole process, so it cannot be observed in-process.
// The test re-execs itself: the gated child runs Run(WithRecover(false)) with a
// panicking service and crashes; the parent asserts the child exited non-zero with
// the panic message in its output.
func TestRun_RecoverDisabled_PanicCrashesProcess(t *testing.T) {
	if os.Getenv("GO_SUPERVISOR_CRASH_CHILD") == "1" {
		_ = supervisor.Run(context.Background(),
			supervisor.WithServiceFunc("boom", func(context.Context) error { panic("kaboom") }),
			supervisor.WithRecover(false),
			supervisor.WithLogger(discardLogger()))
		return // unreachable when the panic propagates as intended
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRun_RecoverDisabled_PanicCrashesProcess$")
	cmd.Env = append(os.Environ(), "GO_SUPERVISOR_CRASH_CHILD=1")
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "child must exit non-zero from the unrecovered panic")
	assert.Contains(t, string(out), "panic: kaboom")
}

func TestRun_InvalidConfigReturnsError(t *testing.T) {
	ok := fakeService{name: "ok", run: func(ctx context.Context) error { return nil }}
	err := supervisor.Run(context.Background(),
		supervisor.WithService(ok),
		supervisor.WithService(nil),
		supervisor.WithShutdownTimeout(-1),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
}

// TestRun_NilRegistration_ReturnsErrInvalidConfig absorbs the former
// TestWithService_NilAppendsError and TestWithServiceFunc_NilFuncAppendsError.
func TestRun_NilRegistration_ReturnsErrInvalidConfig(t *testing.T) {
	cases := map[string]supervisor.Option{
		"nil service": supervisor.WithService(nil),
		"nil func":    supervisor.WithServiceFunc("w", nil),
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			err := supervisor.Run(context.Background(), opt, supervisor.WithLogger(discardLogger()))
			require.Error(t, err)
			assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
			assert.NotErrorIs(t, err, supervisor.ErrNoServices, "invalid-config must short-circuit before the no-services check")
		})
	}
}

// TestRun_WithConfigAppliesBlock absorbs the former TestWithConfig_SetsWholeBlock:
// the grace timeout from the WithConfig block must take effect.
func TestRun_WithConfigAppliesBlock(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stuck := fakeService{name: "stuck", run: func(ctx context.Context) error { <-release; return nil }}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error { return nil }}

	err := supervisor.Run(context.Background(),
		supervisor.WithService(stuck), supervisor.WithService(trigger),
		supervisor.WithConfig(supervisor.Config{ShutdownTimeout: 50 * time.Millisecond, Recover: true}),
		supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrShutdownTimeout, "WithConfig must apply ShutdownTimeout from the block")
}

func TestRun_PanicTriggersGracefulShutdown(t *testing.T) {
	siblingDrained := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingDrained)
		return ctx.Err()
	}}
	panicky := fakeService{name: "panicky", run: func(ctx context.Context) error { panic("boom") }}

	err := supervisor.Run(context.Background(),
		supervisor.WithService(sibling), supervisor.WithService(panicky), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrPanic)
	select {
	case <-siblingDrained:
	case <-time.After(time.Second):
		t.Fatal("sibling did not drain when the other service panicked")
	}
}
```

- [ ] **Step 3: Delete the options test file** — Run: `git rm supervisor/options_test.go`
- [ ] **Step 4: Format** — Run: `just fmt`
- [ ] **Step 5: Run tests** — Run: `just test ./supervisor/`
  Expected: PASS. (`service_test.go` is still `package supervisor` and compiles independently; the subprocess test re-execs the test binary and the child crashes with exit 2.)
- [ ] **Step 6: Lint** — Run: `just lint`
  Expected: clean.
- [ ] **Step 7: Commit**

```bash
git add supervisor/helpers_test.go supervisor/supervisor_test.go supervisor/options_test.go
git commit -m "test(supervisor): make supervisor_test+helpers black-box; subprocess recover test; absorb options_test"
```

---

### Task S4: supervisor/service_test.go → black-box (reframe via WithServiceFunc)

**Files:**
- Modify: `supervisor/service_test.go` (whole file)

**Interfaces:**
- Consumes (from S3, same `supervisor_test` package): `discardLogger`.
- Produces: no shared helpers.

The original built the unexported `serviceFunc` directly; the reframe drives it through the exported `WithServiceFunc` + `Run`. Name propagation is observed via the `"service started"` JSON log record; ctx-passthrough via reading the injected value inside the func; error propagation via `Run`'s returned error.

- [ ] **Step 1: Rewrite the file** — replace the entire contents of `supervisor/service_test.go` with:

```go
package supervisor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/supervisor"
)

func TestWithServiceFunc_DelegatesNameAndRun(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	type ctxKey struct{}
	var gotVal any
	fn := func(ctx context.Context) error {
		gotVal = ctx.Value(ctxKey{})
		return nil
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	err := supervisor.Run(ctx, supervisor.WithServiceFunc("worker", fn), supervisor.WithLogger(logger))
	require.NoError(t, err)

	// gotVal == "v" proves the func ran AND ctx passed straight through.
	assert.Equal(t, "v", gotVal, "fn must be invoked with ctx passed straight through")

	// The name is observable via the structured "service started" log record.
	var named bool
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "service started" && rec["service"] == "worker" {
			named = true
			break
		}
	}
	assert.True(t, named, `expected a "service started" record with service="worker"`)
}

func TestWithServiceFunc_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	fn := func(ctx context.Context) error { return sentinel }
	err := supervisor.Run(context.Background(), supervisor.WithServiceFunc("x", fn), supervisor.WithLogger(discardLogger()))
	require.ErrorIs(t, err, sentinel)
}
```

- [ ] **Step 2: Format** — Run: `just fmt`
- [ ] **Step 3: Run tests** — Run: `just test ./supervisor/`
  Expected: PASS. All `supervisor` test files are now `package supervisor_test`.
- [ ] **Step 4: Verify the flip** — Run: `grep -L "package supervisor_test" supervisor/*_test.go`
  Expected: no output.
- [ ] **Step 5: Lint** — Run: `just lint`
  Expected: clean.
- [ ] **Step 6: Commit**

```bash
git add supervisor/service_test.go
git commit -m "test(supervisor): make service_test black-box via WithServiceFunc"
```

---

### Task T9: Full-suite verification

**Files:** none modified.

- [ ] **Step 1: Confirm no production code changed** — Run:

```bash
git diff --stat main -- 'httpserver/*.go' 'supervisor/*.go' ':!*_test.go'
```

Expected: no output (no non-test `.go` file differs from `main`).

- [ ] **Step 2: Confirm zero white-box test files remain** — Run:

```bash
grep -L "package httpserver_test" httpserver/*_test.go; grep -L "package supervisor_test" supervisor/*_test.go
```

Expected: no output from either grep.

- [ ] **Step 3: Full check** — Run: `just check`
  Expected: `fmt` (no diff), `lint` (0 issues), `test` (all packages PASS under `-race`).

- [ ] **Step 4: Confirm the race test is stable** — Run: `go test -race -run TestRun_SurfacesServeErrorRacingCancel -count=20 ./httpserver/`
  Expected: `ok` (20/20 — the serve-error-racing-cancel assertion never flakes).

- [ ] **Step 5: Commit (if `just fmt` produced any change; otherwise skip)**

```bash
git add -A && git commit -m "test: finalize black-box conversion for httpserver and supervisor" || echo "nothing to commit"
```

## Self-Review

**Spec coverage** (each spec section → task):
- `httpserver/config_test.go` swap → H1. `tls_test.go` swap → H2. `server_test.go` swaps + drain replacement + `TestNew_SeedsDefaults` drop → H3. `options_test.go` reframe + `ConnState` test → H4. `integration_test.go` no-change → untouched (verified by T9 grep, already `httpserver_test`).
- `supervisor/config_test.go` → S1. `context_test.go` + strengthening → S2. `helpers_test.go` move + `supervisor_test.go` reframes + subprocess test + absorbed nil-registration/WithConfig + dropped passthrough → S3. `options_test.go` deletion → S3. `service_test.go` reframe → S4.
- Cross-cutting rules (http.StateNew, import grouping via `just fmt`, nilaway nolint, buffered channels, betteralign, one shared test package) → Global Constraints + applied in H3/H4/S3/S4.
- Acceptance (`just check`, no production diff, grep guard, race stability) → T9.

**Placeholder scan:** none — every step contains full file contents or exact commands with expected output.

**Type/name consistency:** helper names match across tasks — `noopHandler`/`startServed`/`errBoomListener` defined in H3 and consumed in H4; `fakeService` defined in S3 (`helpers_test.go`) and consumed in S3's `supervisor_test.go`; `discardLogger` defined in S3's `supervisor_test.go` and consumed in S4. New test names (`TestRun_SurfacesServeErrorRacingCancel`, `TestRun_ConnStateCallbackFires`, `TestRun_RecoverDisabled_PanicCrashesProcess`, `TestRun_NilRegistration_ReturnsErrInvalidConfig`, `TestRun_WithConfigAppliesBlock`, `TestWithServiceFunc_DelegatesNameAndRun`) are each unique within their package.
