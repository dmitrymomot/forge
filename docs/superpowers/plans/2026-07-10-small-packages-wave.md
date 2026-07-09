# Small-Packages Wave Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship three independent, single-responsibility forge packages — `ops/logsample` (log-volume sampling), `web/ipfilter` (allow/deny IP middleware), and `web/idempotency` (Idempotency-Key middleware).

**Architecture:** Each package consumes only already-shipped seams. `logsample` is an `slog.Handler` decorator (sibling to `ops/logredact`). `ipfilter` is a `middleware.Middleware` over `web/clientip`, rejecting via a `problem.Responder`. `idempotency` is a `middleware.Middleware` riding `resilience/cache`'s atomic SetNX claim, buffering the response to store-and-replay.

**Tech Stack:** Go 1.26, stdlib (`log/slog`, `net/netip`, `net/http`, `crypto/sha256`, `encoding/binary`); forge packages `web/clientip`, `web/middleware`, `web/problem`, `resilience/cache`, `core/errorsx`.

**Spec:** [docs/superpowers/specs/2026-07-10-small-packages-wave-design.md](../specs/2026-07-10-small-packages-wave-design.md)

## Global Constraints

- **Module path:** `github.com/dmitrymomot/forge` — all imports are absolute under it.
- **Go version:** 1.26. Use `new(v)` not a `ptr.To` wrapper; `slog.DiscardHandler` is available.
- **Work only in the current branch** (`dm/small-packages-plans-3eff8a`). Never switch branches.
- **Package anatomy** (design.md): `doc.go` (runnable example) · `options.go` (`type Option func(*config)`, never builders) · `errors.go` (`errors.Is`-matchable single-line sentinels) · impl. No env-loadable `Config` needed for any of these (they are wiring-configured, not env-bootstrapped).
- **Tests are black-box** (`package X_test`) by default; a white-box internal test (`package X`) is allowed *only* to assert an unexported serialization/buffer primitive (used in `idempotency` for `codec`/`capture`).
- **Benchmarks ship with every package**: `Benchmark*` functions calling `b.ReportAllocs()`.
- **Lint cadence:** run `just fmt ./<domain>/<pkg>/...` (package-path form avoids the single-file betteralign quirk) after every task's Go changes. Run `just lint` only at each package's **final task** (and the wave-final step) — NOT after intra-package sub-tasks. A package built across several tasks has helpers (e.g. `defaultMethods`) that are defined before their only caller (`New`) is wired; linting mid-package would trip staticcheck's `unused` (U1000) on a helper that is used one task later. `just fmt` still runs each task, so struct alignment stays correct throughout.
- **No Claude attribution** in any commit message.
- The three packages are **independent** — implement in any order or in parallel. They are listed here in ascending complexity.
- **On ship** (final task of each package), remove that package's entry from `docs/packages.md` — the roadmap lists only unbuilt packages.

---

## Package A — `ops/logsample`

**File structure**
- Create `ops/logsample/options.go` — `config`, `Option`, `WithRate`, `WithMinLevel`.
- Create `ops/logsample/logsample.go` — `New` + the `handler` implementing `slog.Handler`.
- Create `ops/logsample/doc.go` — package doc with runnable example.
- Create `ops/logsample/logsample_test.go` — black-box tests + benchmarks.

### Task A1: sampling handler (options + New + Handle + Enabled)

**Files:**
- Create: `ops/logsample/options.go`
- Create: `ops/logsample/logsample.go`
- Test: `ops/logsample/logsample_test.go`

**Interfaces:**
- Produces: `func New(next slog.Handler, opts ...Option) slog.Handler`; `func WithRate(n int) Option`; `func WithMinLevel(l slog.Level) Option`.

- [ ] **Step 1: Write the failing tests**

Create `ops/logsample/logsample_test.go`:

```go
package logsample_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/ops/logsample"
)

// capture is a test-double slog.Handler that counts records reaching it.
type capture struct {
	mu      sync.Mutex
	records int
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *capture) Handle(_ context.Context, _ slog.Record) error {
	c.mu.Lock()
	c.records++
	c.mu.Unlock()
	return nil
}
func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }
func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records
}

func TestSamplesSubThreshold(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(10), logsample.WithMinLevel(slog.LevelWarn)))
	for range 100 {
		log.Info("noise")
	}
	if got := cap.count(); got != 10 {
		t.Fatalf("kept %d Info records, want 10", got)
	}
}

func TestAlwaysPassesAtOrAboveThreshold(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(10)))
	for range 50 {
		log.Warn("important")
	}
	if got := cap.count(); got != 50 {
		t.Fatalf("kept %d Warn records, want 50", got)
	}
}

func TestRateOneKeepsEverything(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(1)))
	for range 20 {
		log.Info("x")
	}
	if got := cap.count(); got != 20 {
		t.Fatalf("kept %d, want 20", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ops/logsample/`
Expected: FAIL — `undefined: logsample.New` / package not found.

- [ ] **Step 3: Write `options.go`**

```go
package logsample

import "log/slog"

type config struct {
	rate     int
	minLevel slog.Level
}

// Option configures New.
type Option func(*config)

// WithRate keeps 1 of every n sub-threshold records (default 10). n < 1 is
// clamped to 1 (keep everything).
func WithRate(n int) Option {
	return func(c *config) {
		if n < 1 {
			n = 1
		}
		c.rate = n
	}
}

// WithMinLevel sets the level at or above which records always pass unsampled
// (default slog.LevelWarn).
func WithMinLevel(l slog.Level) Option { return func(c *config) { c.minLevel = l } }
```

- [ ] **Step 4: Write `logsample.go`**

```go
package logsample

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// handler samples records below cfg.minLevel and passes the rest to next.
type handler struct {
	next  slog.Handler
	count *atomic.Uint64
	cfg   config
}

// New wraps next so records below the threshold level are sampled "keep 1 of N"
// while records at or above it always pass. Handlers derived via WithAttrs /
// WithGroup share one atomic counter, so a logger and its With children sample
// the same logical stream as one.
func New(next slog.Handler, opts ...Option) slog.Handler {
	c := config{rate: 10, minLevel: slog.LevelWarn}
	for _, o := range opts {
		o(&c)
	}
	return &handler{next: next, count: new(atomic.Uint64), cfg: c}
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= h.cfg.minLevel {
		return h.next.Handle(ctx, r)
	}
	// 1-based counter: keep the 1st, (1+N)th, (1+2N)th, ... sub-threshold record,
	// so the first occurrence of a burst is never lost and rate 1 keeps all.
	n := h.count.Add(1)
	if (n-1)%uint64(h.cfg.rate) == 0 {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{next: h.next.WithAttrs(attrs), count: h.count, cfg: h.cfg}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &handler{next: h.next.WithGroup(name), count: h.count, cfg: h.cfg}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./ops/logsample/`
Expected: PASS (3 tests).

- [ ] **Step 6: Format and commit**

```bash
just fmt ./ops/logsample/...
git add ops/logsample/
git commit -m "feat(logsample): sampling slog.Handler with rate + level threshold"
```

### Task A2: shared-counter derivation + concurrency

**Files:**
- Modify: `ops/logsample/logsample_test.go`

**Interfaces:**
- Consumes: `logsample.New`, `WithRate`, `WithMinLevel`.

- [ ] **Step 1: Add the failing tests**

Append to `ops/logsample/logsample_test.go`:

```go
func TestDerivedHandlerSharesCounter(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(10)))
	child := log.With("k", "v")
	// 5 via parent + 5 via child interleaved = 10 sub-threshold records; at rate 10
	// a shared counter keeps exactly the 1st (=> 1). A per-handler counter would
	// keep the 1st of each => 2.
	for range 5 {
		log.Info("a")
		child.Info("b")
	}
	if got := cap.count(); got != 1 {
		t.Fatalf("kept %d, want 1 (counter must be shared across derived handlers)", got)
	}
}

func TestConcurrentHandleIsDeterministic(t *testing.T) {
	cap := &capture{}
	log := slog.New(logsample.New(cap, logsample.WithRate(4)))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				log.Info("x")
			}
		}()
	}
	wg.Wait()
	// 800 sub-threshold records at rate 4 => (n-1)%4==0 for n=1,5,...,797 => 200.
	if got := cap.count(); got != 200 {
		t.Fatalf("kept %d, want 200", got)
	}
}
```

- [ ] **Step 2: Run to verify they pass**

Run: `go test -race ./ops/logsample/`
Expected: PASS — the implementation from A1 already shares `count` by pointer and uses an atomic, so both tests pass and `-race` is clean. (If `TestDerivedHandlerSharesCounter` fails with `kept 2`, the counter is being copied, not shared — fix `WithAttrs`/`WithGroup` to pass `count: h.count`.)

- [ ] **Step 3: Commit**

```bash
git add ops/logsample/logsample_test.go
git commit -m "test(logsample): shared-counter derivation and concurrency"
```

### Task A3: doc.go + benchmarks

**Files:**
- Create: `ops/logsample/doc.go`
- Modify: `ops/logsample/logsample_test.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package logsample provides an slog.Handler that caps log volume by sampling
// high-frequency records while always passing important ones.
//
// It decorates any slog.Handler: records at or above a threshold level
// (default slog.LevelWarn) always pass; records below it are sampled "keep 1 of
// N" (default 1 of 10) via a shared atomic counter, so a chatty Info/Debug path
// cannot flood the logs or the log bill. Handlers derived via Logger.With share
// the counter and sample the stream as one.
//
//	base := slog.NewJSONHandler(os.Stdout, nil)
//	logger := slog.New(logsample.New(base,
//		logsample.WithRate(100),                // keep 1% of sub-threshold records
//		logsample.WithMinLevel(slog.LevelWarn), // Warn and Error always logged
//	))
//	logger.Info("cache miss")  // sampled
//	logger.Error("db down")    // always logged
package logsample
```

- [ ] **Step 2: Add benchmarks**

Append to `ops/logsample/logsample_test.go`:

```go
func BenchmarkHandleAlwaysPass(b *testing.B) {
	h := logsample.New(slog.DiscardHandler)
	r := slog.NewRecord(time.Time{}, slog.LevelWarn, "msg", 0)
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		_ = h.Handle(ctx, r)
	}
}

func BenchmarkHandleSampled(b *testing.B) {
	h := logsample.New(slog.DiscardHandler, logsample.WithRate(10))
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "msg", 0)
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		_ = h.Handle(ctx, r)
	}
}

func BenchmarkHandleParallel(b *testing.B) {
	h := logsample.New(slog.DiscardHandler, logsample.WithRate(10))
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "msg", 0)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = h.Handle(ctx, r)
		}
	})
}
```

Add `"time"` to the test file's imports.

- [ ] **Step 3: Run tests + benchmarks**

Run: `go test -bench=. -benchmem ./ops/logsample/`
Expected: PASS; `BenchmarkHandleAlwaysPass` and `BenchmarkHandleSampled` report **0 allocs/op**.

- [ ] **Step 4: Lint, drop roadmap entry, commit**

Remove the `**ops/logsample**` entry from `docs/packages.md` (the paragraph under `## ops/`).

```bash
just lint
git add ops/logsample/doc.go ops/logsample/logsample_test.go docs/packages.md
git commit -m "docs(logsample): package doc, benchmarks; drop roadmap entry"
```

---

## Package B — `web/ipfilter`

**File structure**
- Create `web/ipfilter/errors.go` — `ErrInvalidCIDR`, `ErrBlocked`.
- Create `web/ipfilter/options.go` — `config`, `Option`, `WithAllow`, `WithDeny`, `WithClientIP`, `WithResponder`, CIDR parsing.
- Create `web/ipfilter/ipfilter.go` — `New`, the deny-wins evaluator, IP resolution.
- Create `web/ipfilter/doc.go` — package doc with runnable example.
- Create `web/ipfilter/panic_test.go` — panic-contract test (B1).
- Create `web/ipfilter/ipfilter_test.go` — behavior matrix + benchmarks (B2/B3).

### Task B1: full package (parsing, evaluator, middleware) + panic contract

**Files:**
- Create: `web/ipfilter/errors.go`
- Create: `web/ipfilter/options.go`
- Create: `web/ipfilter/ipfilter.go` (complete `New` + deny-wins evaluator + IP resolution)
- Test: `web/ipfilter/panic_test.go`

**Interfaces:**
- Produces: `func New(opts ...Option) middleware.Middleware`; `func WithAllow(cidrs ...string) Option`; `func WithDeny(cidrs ...string) Option`; `func WithClientIP(opts ...clientip.Option) Option`; `func WithResponder(r problem.Responder) Option`; `var ErrInvalidCIDR error`; `var ErrBlocked error`.

- [ ] **Step 1: Write the failing panic-contract test**

Create `web/ipfilter/panic_test.go`:

```go
package ipfilter_test

import (
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/web/ipfilter"
)

func TestInvalidAllowCIDRPanics(t *testing.T) {
	assertPanicsWithErrInvalidCIDR(t, func() {
		ipfilter.New(ipfilter.WithAllow("not-a-cidr"))
	})
}

func TestInvalidDenyCIDRPanics(t *testing.T) {
	assertPanicsWithErrInvalidCIDR(t, func() {
		ipfilter.New(ipfilter.WithDeny("999.999.0.0/16"))
	})
}

func assertPanicsWithErrInvalidCIDR(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got none")
		}
		err, ok := r.(error)
		if !ok || !errors.Is(err, ipfilter.ErrInvalidCIDR) {
			t.Fatalf("want ErrInvalidCIDR, got %v", r)
		}
	}()
	fn()
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./web/ipfilter/`
Expected: FAIL — `undefined: ipfilter.New` / `ipfilter.ErrInvalidCIDR`.

- [ ] **Step 3: Write `errors.go`**

```go
package ipfilter

import "errors"

// ErrInvalidCIDR is wrapped in the panic value New raises when a WithAllow or
// WithDeny entry is not a parseable CIDR or IP address.
var ErrInvalidCIDR = errors.New("ipfilter: invalid CIDR")

// ErrBlocked is passed to the responder when a request's client IP is filtered.
var ErrBlocked = errors.New("ipfilter: client address not allowed")
```

- [ ] **Step 4: Write `options.go`**

```go
package ipfilter

import (
	"fmt"
	"net/netip"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	responder problem.Responder
	allow     []netip.Prefix
	deny      []netip.Prefix
	ipOpts    []clientip.Option
}

// Option configures New.
type Option func(*config)

// WithAllow adds CIDRs or bare IPs to the allowlist. A bare IP becomes a /32
// (IPv4) or /128 (IPv6). New panics (wrapping ErrInvalidCIDR) on an unparseable
// entry.
func WithAllow(cidrs ...string) Option {
	return func(c *config) { c.allow = append(c.allow, mustParse(cidrs)...) }
}

// WithDeny adds CIDRs or bare IPs to the denylist. Deny always wins over allow.
func WithDeny(cidrs ...string) Option {
	return func(c *config) { c.deny = append(c.deny, mustParse(cidrs)...) }
}

// WithClientIP configures how the client IP is resolved (proxy/trust settings).
// Without it, resolution is safe-by-default (RemoteAddr only).
func WithClientIP(opts ...clientip.Option) Option {
	return func(c *config) { c.ipOpts = append(c.ipOpts, opts...) }
}

// WithResponder overrides the rejection response (default problem.JSON 403).
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

func mustParse(cidrs []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		p, err := parsePrefix(s)
		if err != nil {
			panic(fmt.Errorf("%w: %q", ErrInvalidCIDR, s))
		}
		out = append(out, p)
	}
	return out
}

// parsePrefix accepts a CIDR ("10.0.0.0/8") or a bare address ("10.0.0.1"),
// returning a masked prefix so Contains matches correctly.
func parsePrefix(s string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()), nil
}
```

- [ ] **Step 5: Write the complete `ipfilter.go` (New + evaluator)**

`New` calls `parseAddr`, `config.allowed`, and `contains`, so the package leaves no unused functions (B3 runs `just lint` once the package is complete).

```go
package ipfilter

import (
	"net/http"
	"net/netip"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// New returns middleware that allows or blocks requests by client IP using a
// deny-wins model (see WithAllow / WithDeny). The client IP is resolved per
// WithClientIP; a blocked request is answered by the responder (default
// problem.JSON 403). New panics (wrapping ErrInvalidCIDR) on an unparseable
// allow/deny entry — a wiring bug, not a runtime condition.
func New(opts ...Option) middleware.Middleware {
	cfg := config{responder: problem.JSON(problem.WithStatus(http.StatusForbidden))}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr, resolved := parseAddr(clientip.Resolve(r, cfg.ipOpts...))
			if cfg.allowed(addr, resolved) {
				next.ServeHTTP(w, r)
				return
			}
			cfg.responder(w, r, ErrBlocked)
		})
	}
}

// allowed applies the deny-wins model: deny always blocks; a configured
// allowlist is a default-deny gate; with no allowlist everything not denied
// passes. resolved is false when the client IP could not be parsed.
func (c config) allowed(addr netip.Addr, resolved bool) bool {
	if resolved && contains(c.deny, addr) {
		return false
	}
	if len(c.allow) > 0 {
		return resolved && contains(c.allow, addr)
	}
	return true
}

func contains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func parseAddr(s string) (netip.Addr, bool) {
	if s == "" {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}
```

- [ ] **Step 6: Run to verify the panic tests pass**

Run: `go test ./web/ipfilter/`
Expected: PASS (2 panic tests). The behavior matrix is added in Task B2; the implementation is already complete, so B3's `just lint` finds no unused functions.

- [ ] **Step 7: Format and commit**

```bash
just fmt ./web/ipfilter/...
git add web/ipfilter/
git commit -m "feat(ipfilter): allow/deny middleware over clientip with panic-on-bad-CIDR"
```

### Task B2: behavior matrix tests

The implementation is complete after B1; this task adds the full HTTP behavior matrix (allow/deny modes, deny-wins, IPv6, unresolvable IP, proxy trust, responder override) as a second black-box test file. These characterize every branch of `config.allowed` and the resolution/rejection wiring.

**Files:**
- Test: `web/ipfilter/ipfilter_test.go`

**Interfaces:**
- Consumes: `ipfilter.New`, `WithAllow`, `WithDeny`, `WithClientIP`, `WithResponder`.

- [ ] **Step 1: Write the behavior tests**

Create `web/ipfilter/ipfilter_test.go`:

```go
package ipfilter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/ipfilter"
	"github.com/dmitrymomot/forge/web/middleware"
)

func serve(t *testing.T, mw middleware.Middleware, r *http.Request) int {
	t.Helper()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

func reqFrom(remote string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	return r
}

func TestAllowlistGate(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24", "198.51.100.7"))
	if c := serve(t, mw, reqFrom("203.0.113.42:9")); c != http.StatusOK {
		t.Fatalf("in-range: %d", c)
	}
	if c := serve(t, mw, reqFrom("198.51.100.7:9")); c != http.StatusOK {
		t.Fatalf("bare IP: %d", c)
	}
	if c := serve(t, mw, reqFrom("8.8.8.8:9")); c != http.StatusForbidden {
		t.Fatalf("outsider: %d, want 403", c)
	}
}

func TestDenylistOnly(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithDeny("192.0.2.0/24"))
	if c := serve(t, mw, reqFrom("192.0.2.15:9")); c != http.StatusForbidden {
		t.Fatalf("denied: %d, want 403", c)
	}
	if c := serve(t, mw, reqFrom("8.8.8.8:9")); c != http.StatusOK {
		t.Fatalf("not denied: %d", c)
	}
}

func TestDenyWinsOverAllow(t *testing.T) {
	mw := ipfilter.New(
		ipfilter.WithAllow("203.0.113.0/24"),
		ipfilter.WithDeny("203.0.113.66"),
	)
	if c := serve(t, mw, reqFrom("203.0.113.10:9")); c != http.StatusOK {
		t.Fatalf("allowed in range: %d", c)
	}
	if c := serve(t, mw, reqFrom("203.0.113.66:9")); c != http.StatusForbidden {
		t.Fatalf("deny should win: %d, want 403", c)
	}
	if c := serve(t, mw, reqFrom("8.8.8.8:9")); c != http.StatusForbidden {
		t.Fatalf("gate default-deny: %d, want 403", c)
	}
}

func TestIPv6Deny(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithDeny("2001:db8::/32"))
	if c := serve(t, mw, reqFrom("[2001:db8::1]:9")); c != http.StatusForbidden {
		t.Fatalf("ipv6 deny: %d, want 403", c)
	}
}

func TestUnresolvableUnderAllowlistBlocked(t *testing.T) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	r := reqFrom("garbage-not-an-ip")
	if c := serve(t, mw, r); c != http.StatusForbidden {
		t.Fatalf("unresolvable under allowlist: %d, want 403", c)
	}
}

func TestClientIPProxyTrust(t *testing.T) {
	mw := ipfilter.New(
		ipfilter.WithAllow("203.0.113.0/24"),
		ipfilter.WithClientIP(clientip.XRealIP()),
	)
	r := reqFrom("10.0.0.1:9")
	r.Header.Set("X-Real-IP", "203.0.113.9")
	if c := serve(t, mw, r); c != http.StatusOK {
		t.Fatalf("trusted header should allow: %d", c)
	}

	// Without WithClientIP the header is ignored (RemoteAddr only) => blocked.
	mwDefault := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	r2 := reqFrom("10.0.0.1:9")
	r2.Header.Set("X-Real-IP", "203.0.113.9")
	if c := serve(t, mwDefault, r2); c != http.StatusForbidden {
		t.Fatalf("untrusted header must be ignored: %d, want 403", c)
	}
}

func TestResponderOverride(t *testing.T) {
	mw := ipfilter.New(
		ipfilter.WithDeny("0.0.0.0/0"),
		ipfilter.WithResponder(func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusTeapot)
		}),
	)
	if c := serve(t, mw, reqFrom("1.2.3.4:9")); c != http.StatusTeapot {
		t.Fatalf("custom responder: %d, want 418", c)
	}
}
```

- [ ] **Step 2: Run to verify they pass**

Run: `go test ./web/ipfilter/`
Expected: PASS — `New` is already complete from B1, so the full matrix is green. (If `TestUnresolvableUnderAllowlistBlocked` or `TestClientIPProxyTrust` fail, the bug is in B1's `allowed`/`parseAddr`, not the tests — fix B1.)

- [ ] **Step 3: Format and commit**

```bash
just fmt ./web/ipfilter/...
git add web/ipfilter/ipfilter_test.go
git commit -m "test(ipfilter): full allow/deny behavior matrix"
```

### Task B3: doc.go + benchmarks

**Files:**
- Create: `web/ipfilter/doc.go`
- Modify: `web/ipfilter/ipfilter_test.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package ipfilter provides allow/deny IP middleware over web/clientip for admin
// allowlists, partner IP pinning, and blocklists.
//
// It uses a deny-wins model: a denylist match always blocks; a configured
// allowlist is a default-deny gate (only listed ranges pass); with no allowlist,
// anything not denied passes. The client IP is resolved via clientip.Resolve, so
// proxy/trust settings are explicit via WithClientIP. Blocked requests are
// answered by a problem.Responder (default problem.JSON 403). Invalid CIDRs make
// New panic — they are wiring bugs.
//
//	mux.Handle("/admin/", ipfilter.New(
//		ipfilter.WithAllow("203.0.113.0/24"), // office range
//		ipfilter.WithDeny("203.0.113.66"),    // one compromised host inside it
//		ipfilter.WithClientIP(clientip.Cloudflare()),
//	)(adminHandler))
package ipfilter
```

- [ ] **Step 2: Add benchmarks**

Append to `web/ipfilter/ipfilter_test.go`:

```go
func benchServe(b *testing.B, mw middleware.Middleware, r *http.Request) {
	b.Helper()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for range b.N {
		h.ServeHTTP(rec, r)
	}
}

func BenchmarkServeAllowed(b *testing.B) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	benchServe(b, mw, reqFrom("203.0.113.9:9"))
}

func BenchmarkServeBlocked(b *testing.B) {
	mw := ipfilter.New(ipfilter.WithAllow("203.0.113.0/24"))
	benchServe(b, mw, reqFrom("8.8.8.8:9"))
}

func BenchmarkServeLargeList(b *testing.B) {
	cidrs := make([]string, 0, 100)
	for i := range 100 {
		cidrs = append(cidrs, fmt.Sprintf("10.%d.0.0/16", i))
	}
	mw := ipfilter.New(ipfilter.WithAllow(cidrs...))
	benchServe(b, mw, reqFrom("10.99.0.1:9")) // matches the last entry (worst case)
}
```

Add `"fmt"` to the test file's imports.

- [ ] **Step 3: Run tests + benchmarks**

Run: `go test -bench=. -benchmem ./web/ipfilter/`
Expected: PASS; `BenchmarkServeBlocked` reports **0 allocs/op** (blocked path calls the responder; allowed path is also alloc-free for the match — any allocs trace to `clientip.Resolve`, which is expected and worth noting in the commit if nonzero).

- [ ] **Step 4: Lint, drop roadmap entry, commit**

Remove the `**web/ipfilter**` entry from `docs/packages.md`.

```bash
just lint
git add web/ipfilter/doc.go web/ipfilter/ipfilter_test.go docs/packages.md
git commit -m "docs(ipfilter): package doc, benchmarks; drop roadmap entry"
```

---

## Package C — `web/idempotency`

**File structure**
- Create `web/idempotency/errors.go` — coded sentinels + `ErrCorruptRecord`.
- Create `web/idempotency/options.go` — `config`, `Option`s.
- Create `web/idempotency/codec.go` — length-prefixed stored-record encode/decode.
- Create `web/idempotency/capture.go` — buffering `http.ResponseWriter`.
- Create `web/idempotency/idempotency.go` — `New` + `defaultMethods` + flow helpers (fingerprint, readCapped, replay, reject, filterHeader).
- Create `web/idempotency/doc.go` — package doc with runnable example.
- Create `web/idempotency/codec_internal_test.go` — white-box codec round-trip (justified: unexported serialization primitive).
- Create `web/idempotency/capture_internal_test.go` — white-box capture behavior (justified: unexported buffer primitive).
- Create `web/idempotency/idempotency_test.go` — black-box middleware tests + benchmarks.

### Task C1: errors + options + stored-record codec

**Files:**
- Create: `web/idempotency/errors.go`
- Create: `web/idempotency/options.go`
- Create: `web/idempotency/codec.go`
- Test: `web/idempotency/codec_internal_test.go`

**Interfaces:**
- Produces: coded sentinels `ErrKeyRequired`, `ErrInProgress`, `ErrKeyReuse`, `ErrRequestTooLarge`, `ErrReadBody`, plus `ErrCorruptRecord`; `config` + `Option`s (`WithHeader`, `WithMethods`, `WithTTL`, `WithProcessingTTL`, `WithMaxBodySize`, `WithRequireKey`); codec `kindProcessing`/`kindDone`, `type stored`, `encodeProcessing()`, `encodeDone(fp [32]byte, status int, header http.Header, body []byte) []byte`, `decode([]byte) (stored, error)`.

- [ ] **Step 1: Write the failing codec test**

Create `web/idempotency/codec_internal_test.go`:

```go
package idempotency

import (
	"errors"
	"net/http"
	"testing"
)

func TestEncodeDecodeDone(t *testing.T) {
	fp := [32]byte{1, 2, 3, 31: 9}
	hdr := http.Header{"Content-Type": {"application/json"}, "X-Multi": {"a", "b"}}
	got, err := decode(encodeDone(fp, http.StatusCreated, hdr, []byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != kindDone {
		t.Fatalf("kind = %d, want done", got.kind)
	}
	if got.fp != fp {
		t.Fatalf("fingerprint mismatch")
	}
	if got.status != http.StatusCreated {
		t.Fatalf("status = %d", got.status)
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type lost")
	}
	if len(got.header["X-Multi"]) != 2 {
		t.Fatalf("multi-value header lost: %v", got.header["X-Multi"])
	}
	if string(got.body) != "hello" {
		t.Fatalf("body = %q", got.body)
	}
}

func TestDecodeProcessing(t *testing.T) {
	got, err := decode(encodeProcessing())
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != kindProcessing {
		t.Fatalf("want processing marker")
	}
}

func TestDecodeCorrupt(t *testing.T) {
	if _, err := decode(nil); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("nil: %v", err)
	}
	if _, err := decode([]byte{kindDone, 0x01, 0x02}); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("truncated: %v", err)
	}
	if _, err := decode([]byte{0x7f}); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("unknown kind: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./web/idempotency/`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Write `errors.go`**

```go
package idempotency

import (
	"errors"

	"github.com/dmitrymomot/forge/core/errorsx"
)

// Rejection sentinels. Each carries a stable code surfaced as problem+json Code.
var (
	ErrKeyRequired     = errorsx.New("idempotency_key_required", "idempotency key required")
	ErrInProgress      = errorsx.New("idempotency_in_progress", "a request with this idempotency key is already in progress")
	ErrKeyReuse        = errorsx.New("idempotency_key_reuse", "idempotency key reused with a different request payload")
	ErrRequestTooLarge = errorsx.New("idempotency_request_too_large", "request body exceeds the idempotency size limit")
	ErrReadBody        = errorsx.New("idempotency_read_body", "could not read request body")
)

// ErrCorruptRecord marks a stored record that failed to decode. Treated as an
// in-progress claim rather than a 500, so a poisoned entry cannot wedge a key.
var ErrCorruptRecord = errors.New("idempotency: corrupt stored record")
```

- [ ] **Step 4: Write `options.go`**

```go
package idempotency

import "time"

type config struct {
	methods       map[string]bool
	header        string
	ttl           time.Duration
	processingTTL time.Duration
	maxBody       int64
	requireKey    bool
}

// Option configures New.
type Option func(*config)

// WithHeader overrides the idempotency key header (default "Idempotency-Key").
func WithHeader(name string) Option {
	return func(c *config) {
		if name != "" {
			c.header = name
		}
	}
}

// WithMethods overrides the guarded HTTP methods (default POST, PUT, PATCH, DELETE).
func WithMethods(m ...string) Option {
	return func(c *config) {
		set := make(map[string]bool, len(m))
		for _, x := range m {
			set[x] = true
		}
		c.methods = set
	}
}

// WithTTL sets how long a stored response stays replayable (default 24h).
func WithTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// WithProcessingTTL sets the lifetime of the in-flight claim marker (default 1m).
// A crashed first request's key auto-releases after this window.
func WithProcessingTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.processingTTL = d
		}
	}
}

// WithMaxBodySize caps the request body read for fingerprinting and the response
// body buffered for storage (default 1 MiB). Oversize requests get 413; oversize
// responses are sent to the client but not cached.
func WithMaxBodySize(n int64) Option {
	return func(c *config) {
		if n > 0 {
			c.maxBody = n
		}
	}
}

// WithRequireKey makes a guarded request without the key header fail with 400
// instead of passing through unguarded.
func WithRequireKey() Option { return func(c *config) { c.requireKey = true } }
```

> `defaultMethods()` lives in `idempotency.go` (Task C3) next to its only caller `New`, so `options.go` stays free of a helper that would be unused until C3.

- [ ] **Step 5: Write `codec.go`**

```go
package idempotency

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
)

// Stored-record kinds. A processing marker is a single byte; a done record holds
// the fingerprint, status, filtered headers, and body.
const (
	kindProcessing byte = 0
	kindDone       byte = 1
)

type stored struct {
	header http.Header
	body   []byte
	fp     [32]byte
	status int
	kind   byte
}

func encodeProcessing() []byte { return []byte{kindProcessing} }

// encodeDone serializes a completed response with a length-prefixed layout:
// kind | fp[32] | status(u32) | pairCount(u32) | (keyLen key valLen val)* | bodyLen(u32) | body.
func encodeDone(fp [32]byte, status int, header http.Header, body []byte) []byte {
	var b bytes.Buffer
	b.WriteByte(kindDone)
	b.Write(fp[:])
	writeUint32(&b, uint32(status))
	pairs := 0
	for _, vs := range header {
		pairs += len(vs)
	}
	writeUint32(&b, uint32(pairs))
	for k, vs := range header {
		for _, v := range vs {
			writeString(&b, k)
			writeString(&b, v)
		}
	}
	writeUint32(&b, uint32(len(body)))
	b.Write(body)
	return b.Bytes()
}

func decode(data []byte) (stored, error) {
	if len(data) == 0 {
		return stored{}, ErrCorruptRecord
	}
	switch data[0] {
	case kindProcessing:
		return stored{kind: kindProcessing}, nil
	case kindDone:
		// fall through to parse below
	default:
		return stored{}, ErrCorruptRecord
	}

	r := bytes.NewReader(data[1:])
	s := stored{kind: kindDone, header: http.Header{}}
	if _, err := io.ReadFull(r, s.fp[:]); err != nil {
		return stored{}, ErrCorruptRecord
	}
	status, err := readUint32(r)
	if err != nil {
		return stored{}, ErrCorruptRecord
	}
	s.status = int(status)
	pairs, err := readUint32(r)
	if err != nil {
		return stored{}, ErrCorruptRecord
	}
	for range pairs {
		k, err := readString(r)
		if err != nil {
			return stored{}, ErrCorruptRecord
		}
		v, err := readString(r)
		if err != nil {
			return stored{}, ErrCorruptRecord
		}
		s.header.Add(k, v)
	}
	blen, err := readUint32(r)
	if err != nil {
		return stored{}, ErrCorruptRecord
	}
	s.body = make([]byte, blen)
	if _, err := io.ReadFull(r, s.body); err != nil {
		return stored{}, ErrCorruptRecord
	}
	return s, nil
}

func writeUint32(b *bytes.Buffer, n uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], n)
	b.Write(tmp[:])
}

func writeString(b *bytes.Buffer, s string) {
	writeUint32(b, uint32(len(s)))
	b.WriteString(s)
}

func readUint32(r *bytes.Reader) (uint32, error) {
	var tmp [4]byte
	if _, err := io.ReadFull(r, tmp[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(tmp[:]), nil
}

func readString(r *bytes.Reader) (string, error) {
	n, err := readUint32(r)
	if err != nil {
		return "", err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}
```

- [ ] **Step 6: Run to verify the codec tests pass**

Run: `go test ./web/idempotency/ -run 'Decode|EncodeDecode'`
Expected: PASS (3 tests).

- [ ] **Step 7: Format and commit**

```bash
just fmt ./web/idempotency/...
git add web/idempotency/errors.go web/idempotency/options.go web/idempotency/codec.go web/idempotency/codec_internal_test.go
git commit -m "feat(idempotency): sentinels, options, stored-record codec"
```

### Task C2: buffering ResponseWriter

**Files:**
- Create: `web/idempotency/capture.go`
- Test: `web/idempotency/capture_internal_test.go`

**Interfaces:**
- Produces: `type capture` (embeds `http.ResponseWriter`, fields `status int`, `wrote bool`, `limit int64`, `buf bytes.Buffer`, `over bool`); `func (c *capture) finalStatus() int`; `func (c *capture) flush()`.

- [ ] **Step 1: Write the failing tests**

Create `web/idempotency/capture_internal_test.go`:

```go
package idempotency

import (
	"io"
	"net/http/httptest"
	"testing"
)

func TestCaptureBuffersThenFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 1024}
	c.WriteHeader(201)
	_, _ = io.WriteString(c, "body")

	if rec.Body.Len() != 0 {
		t.Fatal("must buffer, not write through before flush")
	}
	if c.finalStatus() != 201 {
		t.Fatalf("finalStatus = %d", c.finalStatus())
	}
	c.flush()
	if rec.Code != 201 || rec.Body.String() != "body" {
		t.Fatalf("after flush: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestCaptureImplicit200(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 1024}
	_, _ = io.WriteString(c, "x") // no explicit WriteHeader
	if c.finalStatus() != 200 {
		t.Fatalf("implicit status = %d, want 200", c.finalStatus())
	}
}

func TestCaptureOverflowStreams(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 4}
	c.WriteHeader(200)
	_, _ = io.WriteString(c, "12345") // exceeds limit 4
	if !c.over {
		t.Fatal("should have flipped to overflow mode")
	}
	if rec.Body.String() != "12345" {
		t.Fatalf("overflow should stream through: %q", rec.Body.String())
	}
	c.flush() // must be a no-op in overflow mode
	if rec.Body.String() != "12345" {
		t.Fatalf("flush double-wrote: %q", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./web/idempotency/ -run Capture`
Expected: FAIL — `undefined: capture`.

- [ ] **Step 3: Write `capture.go`**

```go
package idempotency

import (
	"bytes"
	"net/http"
)

// capture buffers a handler's response so the middleware can decide whether to
// store it before flushing to the client. Header() passes through to the wrapped
// writer, so headers stay uncommitted until flush. If the body exceeds limit it
// flips to overflow mode: the buffered bytes are streamed out and nothing is
// cached.
type capture struct {
	http.ResponseWriter
	buf    bytes.Buffer
	limit  int64
	status int
	wrote  bool
	over   bool
}

func (c *capture) WriteHeader(status int) {
	if c.wrote {
		return
	}
	c.status = status
	c.wrote = true
}

func (c *capture) Write(p []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	if c.over {
		return c.ResponseWriter.Write(p)
	}
	if int64(c.buf.Len())+int64(len(p)) > c.limit {
		c.over = true
		c.ResponseWriter.WriteHeader(c.status)
		if c.buf.Len() > 0 {
			if _, err := c.ResponseWriter.Write(c.buf.Bytes()); err != nil {
				c.buf.Reset()
				return 0, err
			}
			c.buf.Reset()
		}
		return c.ResponseWriter.Write(p)
	}
	return c.buf.Write(p)
}

// finalStatus reports the status the handler set, defaulting to 200 if it wrote
// nothing.
func (c *capture) finalStatus() int {
	if !c.wrote {
		return http.StatusOK
	}
	return c.status
}

// flush writes the buffered response to the underlying writer. It is a no-op in
// overflow mode, where the response has already been streamed.
func (c *capture) flush() {
	if c.over {
		return
	}
	if !c.wrote {
		c.status = http.StatusOK
	}
	c.ResponseWriter.WriteHeader(c.status)
	if c.buf.Len() > 0 {
		_, _ = c.ResponseWriter.Write(c.buf.Bytes())
	}
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./web/idempotency/ -run Capture`
Expected: PASS (3 tests).

- [ ] **Step 5: Format and commit**

```bash
just fmt ./web/idempotency/...
git add web/idempotency/capture.go web/idempotency/capture_internal_test.go
git commit -m "feat(idempotency): buffering response writer with overflow streaming"
```

### Task C3: the middleware (claim, replay, 409/422, release)

**Files:**
- Create: `web/idempotency/idempotency.go`
- Test: `web/idempotency/idempotency_test.go`

**Interfaces:**
- Consumes: everything from C1/C2; `cache.Store`, `cache.WithSetNonExist`, `cache.WithTTL`, `cache.ErrExists`; `middleware.Middleware`; `problem.JSON`, `problem.WithStatusOf`.
- Produces: `func New(store cache.Store, opts ...Option) middleware.Middleware`.

- [ ] **Step 1: Write the failing black-box tests**

Create `web/idempotency/idempotency_test.go`:

```go
package idempotency_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/idempotency"
)

func okJSON() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":1}`)
	})
}

func req(method, target, body, key string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	return r
}

func TestReplaysStoredResponse(t *testing.T) {
	var calls int32
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":1}`)
	}))

	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/orders", `{"x":1}`, "abc"))
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/orders", `{"x":1}`, "abc"))

	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
	if r2.Code != http.StatusCreated {
		t.Fatalf("replay status = %d", r2.Code)
	}
	if r2.Body.String() != `{"id":1}` {
		t.Fatalf("replay body = %q", r2.Body.String())
	}
	if r2.Header().Get("Content-Type") != "application/json" {
		t.Fatal("replay lost Content-Type header")
	}
}

func TestDifferentPayloadRejected(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore())(okJSON())
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{"a":1}`, "k"))
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{"a":2}`, "k"))
	if r2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422", r2.Code)
	}
}

func TestConcurrentInFlightConflict(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	go h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	<-started
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{}`, "k"))
	if r2.Code != http.StatusConflict {
		t.Fatalf("in-flight got %d, want 409", r2.Code)
	}
	close(release)
}

func TestUnguardedMethodPassThrough(t *testing.T) {
	var calls int32
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	for range 3 {
		h.ServeHTTP(httptest.NewRecorder(), req("GET", "/p", "", "k"))
	}
	if calls != 3 {
		t.Fatalf("GET must not be deduped, calls=%d", calls)
	}
}

func TestMissingKey(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore())(okJSON())
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req("POST", "/p", `{}`, ""))
	if r.Code != http.StatusCreated {
		t.Fatalf("default missing-key got %d, want pass-through 201", r.Code)
	}

	h2 := idempotency.New(cache.NewMemoryStore(), idempotency.WithRequireKey())(okJSON())
	r2 := httptest.NewRecorder()
	h2.ServeHTTP(r2, req("POST", "/p", `{}`, ""))
	if r2.Code != http.StatusBadRequest {
		t.Fatalf("required missing-key got %d, want 400", r2.Code)
	}
}

func TestServerErrorReleasesClaim(t *testing.T) {
	var calls int32
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/p", `{}`, "k"))
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{}`, "k"))

	if r1.Code != http.StatusInternalServerError {
		t.Fatalf("first = %d", r1.Code)
	}
	if r2.Code != http.StatusOK {
		t.Fatalf("retry after 5xx should re-run, got %d", r2.Code)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestSetCookieNotReplayed(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "secret"})
		w.WriteHeader(http.StatusOK)
	}))
	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/p", `{}`, "k"))
	if r1.Header().Get("Set-Cookie") == "" {
		t.Fatal("first request should carry Set-Cookie")
	}
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req("POST", "/p", `{}`, "k"))
	if r2.Header().Get("Set-Cookie") != "" {
		t.Fatal("replayed response must not carry Set-Cookie")
	}
}

func TestOversizeRequestRejected(t *testing.T) {
	h := idempotency.New(cache.NewMemoryStore(), idempotency.WithMaxBodySize(1024))(okJSON())
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req("POST", "/p", strings.Repeat("a", 2048), "k"))
	if r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", r.Code)
	}
}

func TestOversizeResponseNotCached(t *testing.T) {
	var calls int32
	body := strings.Repeat("b", 2048)
	h := idempotency.New(cache.NewMemoryStore(), idempotency.WithMaxBodySize(1024))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, req("POST", "/p", `{}`, "k"))
	if r1.Body.String() != body {
		t.Fatal("client must still receive the full oversize body")
	}
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{}`, "k"))
	if calls != 2 {
		t.Fatalf("oversize response should not be cached; calls=%d, want 2", calls)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./web/idempotency/ -run 'Replay|Payload|InFlight|PassThrough|MissingKey|ReleasesClaim|SetCookie|Oversize'`
Expected: FAIL — `undefined: idempotency.New`.

- [ ] **Step 3: Write `idempotency.go`**

```go
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

func defaultMethods() map[string]bool {
	return map[string]bool{
		http.MethodPost:   true,
		http.MethodPut:    true,
		http.MethodPatch:  true,
		http.MethodDelete: true,
	}
}

// New returns Idempotency-Key middleware backed by store. On a guarded method,
// the first request with a given key atomically claims it and its response
// (status < 500) is stored and replayed to later retries; a concurrent duplicate
// gets 409; the same key with a different payload gets 422; a 5xx releases the
// claim so a genuine retry re-executes. The memory cache.Store is LRU-evicting
// and unsuitable in production — use cache/redis or another durable Store.
func New(store cache.Store, opts ...Option) middleware.Middleware {
	cfg := config{
		header:        "Idempotency-Key",
		methods:       defaultMethods(),
		ttl:           24 * time.Hour,
		processingTTL: time.Minute,
		maxBody:       1 << 20,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.methods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get(cfg.header)
			if key == "" {
				if cfg.requireKey {
					reject(w, r, ErrKeyRequired)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			body, tooBig, err := readCapped(r, cfg.maxBody)
			if err != nil {
				reject(w, r, ErrReadBody)
				return
			}
			if tooBig {
				reject(w, r, ErrRequestTooLarge)
				return
			}
			fp := fingerprint(r.Method, r.URL.Path, body)

			ctx := r.Context()
			err = store.Set(ctx, key, encodeProcessing(), cache.WithSetNonExist(), cache.WithTTL(cfg.processingTTL))
			switch {
			case errors.Is(err, cache.ErrExists):
				handleExisting(w, r, store, key, fp)
				return
			case err != nil:
				// Store unavailable: cannot guarantee idempotency; execute once.
				next.ServeHTTP(w, r)
				return
			}

			cw := &capture{ResponseWriter: w, limit: cfg.maxBody}
			completed := false
			defer func() {
				if !completed {
					// panic before completion — release the claim, then re-panic.
					_ = store.Delete(context.WithoutCancel(ctx), key)
				}
			}()
			next.ServeHTTP(cw, r)
			completed = true

			status := cw.finalStatus()
			switch {
			case cw.over:
				// Too large to cache; response already streamed to the client.
				_ = store.Delete(ctx, key)
			case status >= 200 && status < 500:
				// Deterministic outcome (2xx/3xx/4xx) — freeze and replay.
				rec := encodeDone(fp, status, filterHeader(cw.Header()), cw.buf.Bytes())
				_ = store.Set(ctx, key, rec, cache.WithTTL(cfg.ttl))
				cw.flush()
			default:
				// 5xx (or 1xx) — release so a retry actually re-executes.
				_ = store.Delete(ctx, key)
				cw.flush()
			}
		})
	}
}

func handleExisting(w http.ResponseWriter, r *http.Request, store cache.Store, key string, fp [32]byte) {
	data, err := store.Get(r.Context(), key)
	if err != nil {
		// Marker vanished between Set and Get (expiry/race) — safest answer.
		reject(w, r, ErrInProgress)
		return
	}
	rec, err := decode(data)
	if err != nil || rec.kind == kindProcessing {
		reject(w, r, ErrInProgress)
		return
	}
	if rec.fp != fp {
		reject(w, r, ErrKeyReuse)
		return
	}
	replay(w, rec)
}

func replay(w http.ResponseWriter, rec stored) {
	h := w.Header()
	for k, vs := range rec.header {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body)
}

func fingerprint(method, path string, body []byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write([]byte(method))
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write(body)
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// readCapped reads up to limit bytes of the request body for fingerprinting and
// restores r.Body for the handler. tooBig is true when the body exceeds limit.
func readCapped(r *http.Request, limit int64) (body []byte, tooBig bool, err error) {
	if r.Body == nil {
		return nil, false, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > limit {
		return nil, true, nil
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return buf, false, nil
}

// hopByHop headers plus Set-Cookie are never stored or replayed. Replaying a
// rotated auth cookie to a later retry would be unsafe.
var hopByHop = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
	"Set-Cookie":          {},
}

func filterHeader(src http.Header) http.Header {
	out := make(http.Header, len(src))
	for k, vs := range src {
		if _, skip := hopByHop[http.CanonicalHeaderKey(k)]; skip {
			continue
		}
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func reject(w http.ResponseWriter, r *http.Request, err error) {
	problem.JSON(problem.WithStatusOf(statusOf))(w, r, err)
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, ErrKeyRequired), errors.Is(err, ErrReadBody):
		return http.StatusBadRequest
	case errors.Is(err, ErrInProgress):
		return http.StatusConflict
	case errors.Is(err, ErrKeyReuse):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrRequestTooLarge):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}
```

> Note: the cache rule is `status >= 200 && status < 500` (2xx/3xx/4xx), a deliberate superset of the spec's "2xx and 4xx" — 3xx redirects (e.g. a POST→303 See-Other PRG flow) are deterministic and must replay rather than re-execute the mutation. This still never caches 5xx.

- [ ] **Step 4: Run to verify they pass**

Run: `go test -race ./web/idempotency/`
Expected: PASS (all C1/C2/C3 tests), `-race` clean.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./web/idempotency/...
git add web/idempotency/idempotency.go web/idempotency/idempotency_test.go
git commit -m "feat(idempotency): Idempotency-Key middleware over cache.Store"
```

### Task C4: doc.go + benchmarks

**Files:**
- Create: `web/idempotency/doc.go`
- Modify: `web/idempotency/idempotency_test.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package idempotency provides Idempotency-Key middleware for mutating,
// partner-facing API calls: it replays the first response to a retry, rejects a
// concurrent in-flight duplicate with 409, and rejects a key reused with a
// different payload with 422.
//
// The first request with a given key atomically claims it (cache.Store SetNX)
// and, if its status is < 500, its status/headers/body are stored under a TTL
// and replayed to later retries. A 5xx releases the claim so a genuine retry
// re-executes. Set-Cookie and hop-by-hop response headers are never replayed.
// The middleware buffers the response, so it is not for streaming endpoints.
//
// The in-memory cache.Store is LRU-evicting and unsuitable here; back it with
// cache/redis or another durable Store in production.
//
//	store := redis.NewStore(rdb) // resilience/cache/redis
//	mux.Handle("/v1/charges", idempotency.New(store,
//		idempotency.WithTTL(24*time.Hour),
//	)(chargeHandler))
package idempotency
```

- [ ] **Step 2: Add benchmarks**

Append to `web/idempotency/idempotency_test.go`:

```go
func BenchmarkReplay(b *testing.B) {
	store := cache.NewMemoryStore()
	h := idempotency.New(store)(okJSON())
	h.ServeHTTP(httptest.NewRecorder(), req("POST", "/p", `{"x":1}`, "k")) // prime

	payload := `{"x":1}`
	b.ReportAllocs()
	for range b.N {
		r := req("POST", "/p", payload, "k")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}

func BenchmarkFirstCall(b *testing.B) {
	h := idempotency.New(cache.NewMemoryStore())(okJSON())
	b.ReportAllocs()
	for i := range b.N {
		// distinct key per iteration so each is a fresh first-call
		r := req("POST", "/p", `{"x":1}`, strconv.Itoa(i))
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}
```

Add `"strconv"` to the test file's imports.

- [ ] **Step 3: Run tests + benchmarks + race**

Run: `go test -race ./web/idempotency/` then `go test -bench=. -benchmem ./web/idempotency/`
Expected: PASS; benchmarks report a stable allocation baseline (nonzero — buffering + hashing + serialization allocate by design).

- [ ] **Step 4: Lint, drop roadmap entry, commit**

Remove the `**web/idempotency**` entry from `docs/packages.md`.

```bash
just lint
git add web/idempotency/doc.go web/idempotency/idempotency_test.go docs/packages.md
git commit -m "docs(idempotency): package doc, benchmarks; drop roadmap entry"
```

---

## Final verification (whole wave)

- [ ] **Step 1: Full build, vet, lint**

Run: `just lint`
Expected: `go vet`, `go build`, and `golangci-lint` (incl. modernize/betteralign/nilaway) all clean across the module.

- [ ] **Step 2: Full test suite with race**

Run: `go test -race ./ops/logsample/ ./web/ipfilter/ ./web/idempotency/`
Expected: all PASS, race-clean.

- [ ] **Step 3: Confirm roadmap is trimmed**

Run: `grep -nE 'logsample|ipfilter|idempotency' docs/packages.md`
Expected: no matches (all three entries removed).

---

## Self-Review Notes (author)

- **Spec coverage:** logsample (rate + minLevel + shared counter + benchmarks) → A1-A3. ipfilter (deny-wins, three modes, unresolvable, IPv6, panic-on-bad-CIDR, WithClientIP/WithResponder, benchmarks) → B1-B3. idempotency (409/422/replay/5xx-release, two TTLs, Set-Cookie exclusion, size caps, require-key, codec, capture, benchmarks) → C1-C4. All spec sections map to a task.
- **Refinement flagged:** idempotency caches `status < 500` (adds 3xx to the spec's 2xx/4xx) — documented inline in C3 Step 3; a strict superset consistent with "deterministic outcomes replay, transient failures retry."
- **Type consistency:** `stored`, `capture`, `config`, `New` signatures, and helper names (`fingerprint`, `readCapped`, `filterHeader`, `statusOf`, `encodeDone`/`decode`) are used identically across C1-C4.
- **Struct field order:** `just fmt` runs betteralign, which may reorder struct fields (`config`, `handler`, `capture`, `stored`) — expected; do not hand-fight it.
- **Build-order safety (lint):** each package is built across several tasks, so `just lint` runs only at the package's final task (A3/B3/C4) — a helper defined before its caller is wired (e.g. `defaultMethods`, moved into `idempotency.go` beside `New`) would otherwise trip staticcheck `unused`. ipfilter's B1 delivers the complete `New` (evaluator wired) so nothing is left unused; the panic contract is tested in B1 and the full behavior matrix in B2.
- **`cap` shadow:** the middleware uses `cw` (not `cap`) for the capture writer to avoid shadowing the builtin in production code.
