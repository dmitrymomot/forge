# hostrouter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone `hostrouter` package: an `http.Handler` that dispatches requests to per-host handlers (exact + single-label wildcard) with a fallback, exposing the matched host/pattern/subdomain to handlers via request context.

**Architecture:** An immutable `Router` built with functional options. `ServeHTTP` normalizes `req.Host`, looks it up in an exact map, then strips one leading label and looks up a wildcard map, else calls the fallback. Matched routes inject a `Match` into the request context via a single-allocation custom context type; a `WithoutMatchContext()` opt-out makes the matched path zero-allocation. Misconfiguration panics at construction with `errors.Is`-matchable sentinels.

**Tech Stack:** Go 1.26, standard library only (`net/http`, `context`, `strings`, `errors`, `fmt`); tests use `testify` only.

**Spec:** `docs/superpowers/specs/2026-06-26-hostrouter-design.md`

## Global Constraints

- Work ONLY on the `main` branch.
- Use `just` recipes for test/lint/format.
- Go 1.26; **no external dependencies** in production code — stdlib only. Tests may use `testify` (already permitted), nothing else.
- Flat layout: a single new top-level directory `hostrouter/`. No nested folders.
- Functional options only — **no builder pattern**.
- Errors are single-line and `errors.Is`-matchable.
- `just fmt` runs `betteralign`, which may reorder struct fields for memory alignment — this is expected; do not fight it.
- Run `just fmt ./hostrouter/...` before each commit and `just lint` as the final gate.

---

### Task 1: Host normalization helpers

**Files:**
- Create: `hostrouter/hostrouter.go`
- Test: `hostrouter/hostrouter_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `normalizeHost(host string) string` — lowercases, strips port, removes IPv6 brackets, trims trailing FQDN dot.
  - `splitFirstLabel(host string) (label, parent string, ok bool)` — splits one leading label; `ok=false` for no/leading/trailing dot.

- [ ] **Step 1: Write the failing tests**

Create `hostrouter/hostrouter_test.go`:

```go
package hostrouter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeHost(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"plain", "example.com", "example.com"},
		{"uppercase", "API.Example.COM", "api.example.com"},
		{"with port", "example.com:8080", "example.com"},
		{"trailing dot", "example.com.", "example.com"},
		{"port and case", "API.example.com:443", "api.example.com"},
		{"ipv6 with port", "[::1]:8080", "::1"},
		{"ipv6 no port", "[::1]", "::1"},
		{"ipv6 bracketless", "::1", "::1"},
		{"subdomain", "foo.example.com", "foo.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeHost(tt.in))
		})
	}
}

func TestSplitFirstLabel(t *testing.T) {
	tests := []struct {
		name, in, label, parent string
		ok                      bool
	}{
		{"single label", "localhost", "", "", false},
		{"two labels", "foo.example.com", "foo", "example.com", true},
		{"three labels", "a.b.example.com", "a", "b.example.com", true},
		{"leading dot", ".example.com", "", "", false},
		{"trailing dot", "example.", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, parent, ok := splitFirstLabel(tt.in)
			assert.Equal(t, tt.label, label)
			assert.Equal(t, tt.parent, parent)
			assert.Equal(t, tt.ok, ok)
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestNormalizeHost|TestSplitFirstLabel' ./hostrouter/ -v`
Expected: FAIL — build error `undefined: normalizeHost`, `undefined: splitFirstLabel`.

- [ ] **Step 3: Write the implementation**

Create `hostrouter/hostrouter.go`:

```go
package hostrouter

import "strings"

// normalizeHost lowercases the host, strips any port, removes IPv6 brackets, and
// trims a trailing FQDN dot. It allocates only on strings.ToLower's slow path
// (uppercase input); an already-lowercase host returns sub-slices with no copy.
//
// net.SplitHostPort is deliberately avoided: it allocates an *AddrError whenever
// the host has no port (the common proxied/HTTP2 case).
func normalizeHost(host string) string {
	if host == "" {
		return ""
	}
	if host[0] == '[' { // IPv6 literal: "[::1]" or "[::1]:8080"
		if i := strings.IndexByte(host, ']'); i >= 0 {
			host = host[1:i] // inside brackets; drops "]" and any ":port" after it
		}
	} else if i := strings.LastIndexByte(host, ':'); i >= 0 &&
		strings.IndexByte(host, ':') == i {
		host = host[:i] // exactly one colon => host:port (not bracketless IPv6)
	}
	host = strings.TrimSuffix(host, ".") // rooted FQDN "example.com."
	return strings.ToLower(host)
}

// splitFirstLabel splits "foo.example.com" into ("foo", "example.com", true).
// ok is false when there is no dot, a leading dot, or a trailing dot.
func splitFirstLabel(host string) (label, parent string, ok bool) {
	i := strings.IndexByte(host, '.')
	if i <= 0 || i == len(host)-1 {
		return "", "", false
	}
	return host[:i], host[i+1:], true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestNormalizeHost|TestSplitFirstLabel' ./hostrouter/ -v`
Expected: PASS (all subtests).

Then the full race build: `just test ./hostrouter/`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./hostrouter/...
git add hostrouter/hostrouter.go hostrouter/hostrouter_test.go
git commit -m "feat(hostrouter): add host normalization helpers"
```

---

### Task 2: Router, options, and routing

**Files:**
- Create: `hostrouter/errors.go`
- Create: `hostrouter/options.go`
- Modify: `hostrouter/hostrouter.go` (add `Router`, `New`, `ServeHTTP`)
- Test: `hostrouter/hostrouter_test.go` (add routing tests)
- Test: `hostrouter/options_test.go`

**Interfaces:**
- Consumes: `normalizeHost`, `splitFirstLabel` (Task 1).
- Produces:
  - `type Router struct { exact map[string]http.Handler; wildcard map[string]http.Handler; fallback http.Handler }` (fields unexported).
  - `func New(opts ...Option) *Router` — seeds empty maps + `http.NotFoundHandler()` fallback, applies options.
  - `func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request)` — exact → single-label wildcard → fallback.
  - `type Option func(*Router)`.
  - `func WithHost(pattern string, h http.Handler) Option` — exact (`"api.example.com"`) or wildcard (`"*.example.com"`); panics on invalid input.
  - `func WithFallback(h http.Handler) Option` — sets fallback; panics if nil; last wins.
  - Sentinels: `ErrNilHandler`, `ErrInvalidPattern`, `ErrDuplicateHost`.

- [ ] **Step 1: Write the failing routing tests**

Add to `hostrouter/hostrouter_test.go` (keep existing tests; add imports `net/http`, `net/http/httptest`):

```go
// handlerWriting returns a handler that writes id to the response body.
func handlerWriting(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(id))
	})
}

func TestRouter_Routing(t *testing.T) {
	r := New(
		WithHost("api.example.com", handlerWriting("api")),
		WithHost("example.com", handlerWriting("apex")),
		WithHost("*.example.com", handlerWriting("wild")),
		WithFallback(handlerWriting("fallback")),
	)
	tests := []struct{ name, host, want string }{
		{"exact wins over wildcard", "api.example.com", "api"},
		{"apex exact", "example.com", "apex"},
		{"wildcard single label", "foo.example.com", "wild"},
		{"wildcard case and port", "FOO.example.com:8443", "wild"},
		{"multi level no match", "a.b.example.com", "fallback"},
		{"unknown host", "other.com", "fallback"},
		{"empty host", "", "fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.Equal(t, tt.want, rec.Body.String())
		})
	}
}

func TestRouter_DefaultFallbackIs404(t *testing.T) {
	r := New(WithHost("api.example.com", handlerWriting("api")))
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "unknown.com"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRouter_NoRoutesAllFallback(t *testing.T) {
	r := New()
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "anything.com"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
```

- [ ] **Step 2: Write the failing panic tests**

Create `hostrouter/options_test.go`:

```go
package hostrouter

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nopHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

// recoverErr runs fn and returns the error it panicked with (nil if no panic).
func recoverErr(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err, _ = r.(error)
		}
	}()
	fn()
	return nil
}

func TestWithHost_Panics(t *testing.T) {
	tests := []struct {
		name     string
		build    func()
		sentinel error
	}{
		{"nil handler", func() { New(WithHost("x.com", nil)) }, ErrNilHandler},
		{"empty pattern", func() { New(WithHost("", nopHandler())) }, ErrInvalidPattern},
		{"bare wildcard", func() { New(WithHost("*.", nopHandler())) }, ErrInvalidPattern},
		{"lone star", func() { New(WithHost("*", nopHandler())) }, ErrInvalidPattern},
		{"double wildcard", func() { New(WithHost("*.*.com", nopHandler())) }, ErrInvalidPattern},
		{"embedded star", func() { New(WithHost("fo*.com", nopHandler())) }, ErrInvalidPattern},
		{"duplicate exact", func() {
			New(WithHost("x.com", nopHandler()), WithHost("x.com", nopHandler()))
		}, ErrDuplicateHost},
		{"duplicate wildcard", func() {
			New(WithHost("*.x.com", nopHandler()), WithHost("*.x.com", nopHandler()))
		}, ErrDuplicateHost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := recoverErr(tt.build)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.sentinel)
		})
	}
}

func TestWithFallback_NilPanics(t *testing.T) {
	err := recoverErr(func() { New(WithFallback(nil)) })
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilHandler)
}

func TestWithHost_NoPanicCases(t *testing.T) {
	// exact + wildcard with the same parent coexist (different maps).
	assert.NotPanics(t, func() {
		New(WithHost("x.com", nopHandler()), WithHost("*.x.com", nopHandler()))
	})
	// repeated WithFallback: last wins, no panic.
	assert.NotPanics(t, func() {
		New(WithFallback(nopHandler()), WithFallback(nopHandler()))
	})
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./hostrouter/ -v`
Expected: FAIL — build errors `undefined: New`, `undefined: WithHost`, `undefined: WithFallback`, `undefined: ErrNilHandler`, etc.

- [ ] **Step 4: Write the sentinel errors**

Create `hostrouter/errors.go`:

```go
package hostrouter

import "errors"

// Sentinel errors used as panic payloads by New (via WithHost / WithFallback) when
// a Router is misconfigured. Recover and match them with errors.Is.
var (
	// ErrNilHandler is used when a nil http.Handler is registered.
	ErrNilHandler = errors.New("hostrouter: nil handler")
	// ErrInvalidPattern is used when a host pattern is malformed.
	ErrInvalidPattern = errors.New("hostrouter: invalid host pattern")
	// ErrDuplicateHost is used when a host pattern is registered more than once.
	ErrDuplicateHost = errors.New("hostrouter: duplicate host pattern")
)
```

- [ ] **Step 5: Write the Router, New, and ServeHTTP**

Add to `hostrouter/hostrouter.go` (add `"net/http"` to the imports; `"strings"` stays):

```go
// Router dispatches by Host header. It is immutable after New and safe for
// concurrent use. It implements http.Handler.
type Router struct {
	exact    map[string]http.Handler
	wildcard map[string]http.Handler
	fallback http.Handler
}

// New builds a Router from options applied in order. With no WithHost options every
// request is served by the fallback (HTTP 404 unless WithFallback overrides it).
// New panics on any invalid registration. It does no I/O.
func New(opts ...Option) *Router {
	r := &Router{
		exact:    make(map[string]http.Handler),
		wildcard: make(map[string]http.Handler),
		fallback: http.NotFoundHandler(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ServeHTTP routes by Host: exact match first, then a single-label wildcard, then
// the fallback.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	if h, ok := r.exact[host]; ok {
		h.ServeHTTP(w, req)
		return
	}
	if _, parent, ok := splitFirstLabel(host); ok {
		if h, found := r.wildcard[parent]; found {
			h.ServeHTTP(w, req)
			return
		}
	}
	r.fallback.ServeHTTP(w, req)
}
```

- [ ] **Step 6: Write the options**

Create `hostrouter/options.go`:

```go
package hostrouter

import (
	"fmt"
	"net/http"
	"strings"
)

// Option configures a Router. Options apply in order and panic on invalid input.
type Option func(*Router)

// WithHost registers handler h for pattern, an exact host ("api.example.com") or a
// single-label wildcard ("*.example.com"). The pattern is normalized identically to
// incoming hosts, so casing/port/trailing-dot never cause a mismatch. It panics
// (ErrNilHandler / ErrInvalidPattern / ErrDuplicateHost) on invalid input.
func WithHost(pattern string, h http.Handler) Option {
	return func(r *Router) {
		if h == nil {
			panic(fmt.Errorf("%w: %q", ErrNilHandler, pattern))
		}
		if strings.HasPrefix(pattern, "*.") {
			parent := normalizeHost(pattern[2:])
			if parent == "" || strings.ContainsRune(parent, '*') {
				panic(fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
			}
			if _, dup := r.wildcard[parent]; dup {
				panic(fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
			}
			r.wildcard[parent] = h
			return
		}
		host := normalizeHost(pattern)
		if host == "" || strings.ContainsRune(host, '*') {
			panic(fmt.Errorf("%w: %q", ErrInvalidPattern, pattern))
		}
		if _, dup := r.exact[host]; dup {
			panic(fmt.Errorf("%w: %q", ErrDuplicateHost, pattern))
		}
		r.exact[host] = h
	}
}

// WithFallback sets the handler for unmatched hosts. The default is
// http.NotFoundHandler() (404). It panics (ErrNilHandler) if h is nil. Last wins.
func WithFallback(h http.Handler) Option {
	return func(r *Router) {
		if h == nil {
			panic(fmt.Errorf("%w: WithFallback handler", ErrNilHandler))
		}
		r.fallback = h
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./hostrouter/ -v`
Expected: PASS (routing + panic tests + Task 1 tests).

Then the full race build: `just test ./hostrouter/`
Expected: PASS.

- [ ] **Step 8: Format and commit**

```bash
just fmt ./hostrouter/...
git add hostrouter/
git commit -m "feat(hostrouter): add Router with exact and wildcard routing"
```

---

### Task 3: Match context injection + zero-alloc opt-out

**Files:**
- Create: `hostrouter/context.go`
- Modify: `hostrouter/hostrouter.go` (add `wildcardEntry` + `injectCtx`; rewrite `New` and `ServeHTTP`; add `serve`)
- Modify: `hostrouter/options.go` (wildcard insert stores `wildcardEntry`; add `WithoutMatchContext`)
- Test: `hostrouter/context_test.go`

**Interfaces:**
- Consumes: `Router`, `New`, `ServeHTTP`, `WithHost` (Task 2); `splitFirstLabel`, `normalizeHost` (Task 1).
- Produces:
  - `type Match struct { Host, Pattern, Subdomain string }`.
  - `func FromContext(ctx context.Context) (Match, bool)`.
  - `func Subdomain(ctx context.Context) string`, `func Pattern(ctx context.Context) string`, `func Host(ctx context.Context) string`.
  - `func WithoutMatchContext() Option`.
  - Internal: `type wildcardEntry struct { handler http.Handler; pattern string }`, `type matchCtx`, `(*Router).serve`, `Router.injectCtx bool`.

- [ ] **Step 1: Write the failing context tests**

Create `hostrouter/context_test.go`:

```go
package hostrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// captureMatch returns a handler that records the Match seen during the request.
func captureMatch(dst *Match, ok *bool) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		*dst, *ok = FromContext(r.Context())
	})
}

// doServe drives a single request for host through r.
func doServe(r *Router, host string) {
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = host
	r.ServeHTTP(httptest.NewRecorder(), req)
}

func TestFromContext_Wildcard(t *testing.T) {
	var got Match
	var ok bool
	r := New(WithHost("*.example.com", captureMatch(&got, &ok)))
	doServe(r, "foo.example.com")

	assert.True(t, ok)
	assert.Equal(t, Match{Host: "foo.example.com", Pattern: "*.example.com", Subdomain: "foo"}, got)
}

func TestFromContext_Exact(t *testing.T) {
	var got Match
	var ok bool
	r := New(WithHost("api.example.com", captureMatch(&got, &ok)))
	doServe(r, "api.example.com")

	assert.True(t, ok)
	assert.Equal(t, Match{Host: "api.example.com", Pattern: "api.example.com", Subdomain: ""}, got)
}

func TestFromContext_FallbackHasNoMatch(t *testing.T) {
	var got Match
	var ok bool
	r := New(WithFallback(captureMatch(&got, &ok)))
	doServe(r, "unknown.com")

	assert.False(t, ok)
	assert.Equal(t, Match{}, got)
}

func TestAccessors(t *testing.T) {
	var sub, pat, host string
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub, pat, host = Subdomain(ctx), Pattern(ctx), Host(ctx)
	})
	r := New(WithHost("*.example.com", h))
	doServe(r, "tenant.example.com")

	assert.Equal(t, "tenant", sub)
	assert.Equal(t, "*.example.com", pat)
	assert.Equal(t, "tenant.example.com", host)
}

func TestWithoutMatchContext_NoInjection(t *testing.T) {
	var got Match
	var ok bool
	r := New(
		WithHost("*.example.com", captureMatch(&got, &ok)),
		WithoutMatchContext(),
	)
	doServe(r, "foo.example.com")

	assert.False(t, ok, "injection disabled, FromContext must report no match")
	assert.Equal(t, Match{}, got)
}

func TestMatch_SurvivesDownstreamContextWrap(t *testing.T) {
	type otherKey struct{}
	var got Match
	var ok bool
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// middleware adds its own value under a different key downstream
		wrapped := context.WithValue(r.Context(), otherKey{}, "x")
		got, ok = FromContext(wrapped)
	})
	r := New(WithHost("*.example.com", h))
	doServe(r, "foo.example.com")

	assert.True(t, ok)
	assert.Equal(t, "foo", got.Subdomain)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'FromContext|Accessors|WithoutMatchContext|SurvivesDownstream' ./hostrouter/ -v`
Expected: FAIL — build errors `undefined: Match`, `undefined: FromContext`, `undefined: Subdomain`, `undefined: WithoutMatchContext`.

- [ ] **Step 3: Write the context file**

Create `hostrouter/context.go`:

```go
package hostrouter

import "context"

// Match describes how a request was routed. The Router injects it into the request
// context (unless WithoutMatchContext is set); read it with FromContext or the
// Subdomain/Pattern/Host accessors.
type Match struct {
	Host      string // normalized host that matched, e.g. "foo.example.com"
	Pattern   string // registered pattern, e.g. "*.example.com" or "api.example.com"
	Subdomain string // captured wildcard label ("foo"); "" for exact matches
}

type ctxKey struct{}

var matchKey = ctxKey{}

// matchCtx carries a Match in a single heap allocation, instead of the two that
// context.WithValue would cost (a *valueCtx node plus boxing the value). Value
// returns the same pointer every call, so reads never allocate.
type matchCtx struct {
	context.Context
	m Match
}

func (c *matchCtx) Value(key any) any {
	if key == matchKey {
		return &c.m
	}
	return c.Context.Value(key)
}

// FromContext returns the Match injected by the Router. ok is false when there was
// no match (the fallback handler) or injection was disabled with WithoutMatchContext.
// The returned Match is a copy; callers cannot mutate the Router's value.
func FromContext(ctx context.Context) (Match, bool) {
	if m, ok := ctx.Value(matchKey).(*Match); ok {
		return *m, true
	}
	return Match{}, false
}

// Subdomain returns the captured wildcard label, or "" if absent.
func Subdomain(ctx context.Context) string { m, _ := FromContext(ctx); return m.Subdomain }

// Pattern returns the matched registered pattern, or "" if absent.
func Pattern(ctx context.Context) string { m, _ := FromContext(ctx); return m.Pattern }

// Host returns the normalized matched host, or "" if absent.
func Host(ctx context.Context) string { m, _ := FromContext(ctx); return m.Host }
```

- [ ] **Step 4: Update hostrouter.go (wildcardEntry, injectCtx, serve, ServeHTTP)**

In `hostrouter/hostrouter.go`, replace the `Router` type, `New`, and `ServeHTTP` from Task 2 with the versions below, and add `serve` and `wildcardEntry`:

```go
// wildcardEntry stores a wildcard handler together with its pre-built "*."+parent
// pattern, so ServeHTTP never concatenates on the hot path.
type wildcardEntry struct {
	handler http.Handler
	pattern string
}

// Router dispatches by Host header. It is immutable after New and safe for
// concurrent use. It implements http.Handler.
type Router struct {
	exact     map[string]http.Handler
	wildcard  map[string]wildcardEntry
	fallback  http.Handler
	injectCtx bool
}

// New builds a Router from options applied in order. With no WithHost options every
// request is served by the fallback (HTTP 404 unless WithFallback overrides it).
// New panics on any invalid registration. It does no I/O.
func New(opts ...Option) *Router {
	r := &Router{
		exact:     make(map[string]http.Handler),
		wildcard:  make(map[string]wildcardEntry),
		fallback:  http.NotFoundHandler(),
		injectCtx: true,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ServeHTTP routes by Host: exact match first, then a single-label wildcard, then
// the fallback. Matched routes carry a Match in the request context unless the
// Router was built with WithoutMatchContext.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	if h, ok := r.exact[host]; ok {
		r.serve(w, req, h, Match{Host: host, Pattern: host})
		return
	}
	if label, parent, ok := splitFirstLabel(host); ok {
		if e, found := r.wildcard[parent]; found {
			r.serve(w, req, e.handler, Match{Host: host, Pattern: e.pattern, Subdomain: label})
			return
		}
	}
	r.fallback.ServeHTTP(w, req) // no match: no context injected, original request
}

// serve dispatches to h, injecting m into the request context unless the Router was
// built with WithoutMatchContext.
func (r *Router) serve(w http.ResponseWriter, req *http.Request, h http.Handler, m Match) {
	if r.injectCtx {
		req = req.WithContext(&matchCtx{Context: req.Context(), m: m})
	}
	h.ServeHTTP(w, req)
}
```

- [ ] **Step 5: Update options.go (wildcard insert + WithoutMatchContext)**

In `hostrouter/options.go`, inside `WithHost`, change the wildcard insert line:

```go
			r.wildcard[parent] = h
```

to:

```go
			r.wildcard[parent] = wildcardEntry{handler: h, pattern: "*." + parent}
```

Then add this option to the end of the file:

```go
// WithoutMatchContext disables match-context injection for the Router, making the
// matched path zero-allocation (no http.Request copy). FromContext and the
// Subdomain/Pattern/Host accessors then return zero values.
func WithoutMatchContext() Option {
	return func(r *Router) { r.injectCtx = false }
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./hostrouter/ -v`
Expected: PASS (all context tests plus every test from Tasks 1–2 still green).

Then the full race build: `just test ./hostrouter/`
Expected: PASS.

- [ ] **Step 7: Format and commit**

```bash
just fmt ./hostrouter/...
git add hostrouter/
git commit -m "feat(hostrouter): inject match context with zero-alloc opt-out"
```

---

### Task 4: Package doc, runnable example, and benchmarks

**Files:**
- Create: `hostrouter/doc.go`
- Create: `hostrouter/example_test.go`
- Create: `hostrouter/bench_test.go`

**Interfaces:**
- Consumes: the full public API (`New`, `WithHost`, `WithFallback`, `WithoutMatchContext`, `Subdomain`, `normalizeHost`) from Tasks 1–3.
- Produces: package documentation, a verifiable `Example`, allocation benchmarks, and two zero-allocation regression tests (`TestZeroAllocFallback`, `TestZeroAllocWithoutMatchContext`).

- [ ] **Step 1: Write the package doc**

Create `hostrouter/doc.go`:

```go
// Package hostrouter routes HTTP requests to different handlers by the request's
// Host header. It supports exact hosts and single-label wildcard subdomains, with a
// configurable fallback, and exposes the matched host/pattern/subdomain to handlers
// via the request context.
//
// A Router is a plain http.Handler, so it composes with httpserver (or any server)
// directly:
//
//	router := hostrouter.New(
//		hostrouter.WithHost("api.example.com", apiMux),
//		hostrouter.WithHost("*.example.com", tenantMux),
//		hostrouter.WithFallback(marketingSite),
//	)
//	srv := httpserver.New(router, httpserver.WithAddr(":8080"))
//
// Matching is exact first, then a single leading label against a "*." wildcard:
// "*.example.com" matches "foo.example.com" but not "a.b.example.com" nor the apex
// "example.com". Misconfiguration (nil handler, malformed or duplicate pattern)
// panics at construction with an errors.Is-matchable sentinel.
//
// Inside a wildcard handler, read the matched subdomain:
//
//	tenant := hostrouter.Subdomain(r.Context()) // "foo" for foo.example.com
package hostrouter
```

- [ ] **Step 2: Write the runnable example**

Create `hostrouter/example_test.go`:

```go
package hostrouter_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/hostrouter"
)

func Example() {
	tenant := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "tenant=%s", hostrouter.Subdomain(r.Context()))
	})
	router := hostrouter.New(
		hostrouter.WithHost("*.example.com", tenant),
	)

	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "acme.example.com"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	// Output: tenant=acme
}
```

- [ ] **Step 3: Write the benchmarks and zero-alloc regression tests**

Create `hostrouter/bench_test.go`:

```go
package hostrouter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func benchHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func benchRequest(host string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = host
	return req
}

func BenchmarkNormalizeHost(b *testing.B) {
	cases := []string{"foo.example.com", "example.com:8080", "[::1]:8080", "API.Example.COM"}
	for _, c := range cases {
		b.Run(c, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = normalizeHost(c)
			}
		})
	}
}

func BenchmarkServeHTTP_Exact(b *testing.B) {
	r := New(WithHost("api.example.com", benchHandler()))
	req, rec := benchRequest("api.example.com"), httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_Wildcard(b *testing.B) {
	r := New(WithHost("*.example.com", benchHandler()))
	req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_WildcardNoContext(b *testing.B) {
	r := New(WithHost("*.example.com", benchHandler()), WithoutMatchContext())
	req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_Fallback(b *testing.B) {
	r := New(WithHost("api.example.com", benchHandler()), WithFallback(benchHandler()))
	req, rec := benchRequest("unknown.com"), httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkServeHTTP_Parallel(b *testing.B) {
	r := New(WithHost("*.example.com", benchHandler()))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
		for pb.Next() {
			r.ServeHTTP(rec, req)
		}
	})
}

// TestZeroAllocFallback locks the no-match path at zero allocations. The fallback is
// a no-op handler so the measurement reflects routing only, not the 404 writer.
func TestZeroAllocFallback(t *testing.T) {
	r := New(WithHost("api.example.com", benchHandler()), WithFallback(benchHandler()))
	req, rec := benchRequest("unknown.com"), httptest.NewRecorder()
	avg := testing.AllocsPerRun(100, func() { r.ServeHTTP(rec, req) })
	assert.Zero(t, avg, "fallback routing must not allocate")
}

// TestZeroAllocWithoutMatchContext locks the matched path at zero allocations when
// context injection is disabled.
func TestZeroAllocWithoutMatchContext(t *testing.T) {
	r := New(WithHost("*.example.com", benchHandler()), WithoutMatchContext())
	req, rec := benchRequest("foo.example.com"), httptest.NewRecorder()
	avg := testing.AllocsPerRun(100, func() { r.ServeHTTP(rec, req) })
	assert.Zero(t, avg, "matched path with WithoutMatchContext must not allocate")
}
```

- [ ] **Step 4: Run tests + example to verify they pass**

Run: `go test ./hostrouter/ -v`
Expected: PASS — including `Example` (verifies `Output: tenant=acme`), `TestZeroAllocFallback`, and `TestZeroAllocWithoutMatchContext`.

- [ ] **Step 5: Run the benchmarks and confirm allocation counts**

Run: `just bench ./hostrouter/`
Expected (with `-benchmem`), confirm the `allocs/op` column:
- `BenchmarkNormalizeHost/foo.example.com`, `/example.com:8080`, `/[::1]:8080` → **0 allocs/op**; `/API.Example.COM` → 1 (ToLower slow path).
- `BenchmarkServeHTTP_Exact`, `BenchmarkServeHTTP_Wildcard` → **2 allocs/op** (request copy + matchCtx).
- `BenchmarkServeHTTP_WildcardNoContext`, `BenchmarkServeHTTP_Fallback` → **0 allocs/op**.

- [ ] **Step 6: Full repo gate**

Run: `just fmt ./hostrouter/...`
Run: `just lint`
Expected: clean (go vet, build, golangci-lint, nilaway, betteralign, modernize all pass).
Run: `just test ./hostrouter/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add hostrouter/
git commit -m "docs(hostrouter): add package doc, example, and benchmarks"
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| Package & module, flat layout, stdlib only, no Config | Tasks 1–4 (files created under `hostrouter/`) |
| `normalizeHost` (no `net.SplitHostPort`, IPv6, port, dot, case) | Task 1 |
| `splitFirstLabel` single-label semantics | Task 1 |
| `Router`, `New`, `ServeHTTP` exact → wildcard → fallback, exact wins | Tasks 2 & 3 |
| `WithHost`, `WithFallback`, default 404 | Task 2 |
| Panic on nil/malformed/duplicate with sentinels | Task 2 |
| `Match`, `FromContext`, `Subdomain`/`Pattern`/`Host` accessors | Task 3 |
| Single-alloc `matchCtx`, pre-built wildcard pattern, `serve` indirection | Task 3 |
| `WithoutMatchContext` zero-alloc opt-out | Task 3 |
| Lock-free immutable Router (no mutex) | Tasks 2–3 (read-only maps) + `BenchmarkServeHTTP_Parallel` |
| Performance: 0 alloc normalize/fallback/opt-out, 2 alloc matched | Task 4 (benchmarks + AllocsPerRun gates) |
| `doc.go` + runnable Example | Task 4 |
| Edge cases (apex, multi-level, empty host, exact+wildcard coexist, IPv6) | Tasks 2–3 tests |

No gaps.

**Placeholder scan:** No TBD/TODO; every code step shows complete code; every command shows expected output. Clean.

**Type consistency:** `normalizeHost`/`splitFirstLabel` signatures match across tasks. `Router` field set evolves additively: Task 2 (`exact`, `wildcard map[string]http.Handler`, `fallback`) → Task 3 (`wildcard map[string]wildcardEntry`, adds `injectCtx`); Task 3 Step 4 rewrites `New`/`ServeHTTP` and Step 5 updates the matching `WithHost` insert, so no stale `map[string]http.Handler` reference remains. `Match{Host, Pattern, Subdomain}` field names are identical in `context.go`, `ServeHTTP`, and every test. `matchKey`/`ctxKey` consistent between `matchCtx.Value` and `FromContext`. `WithoutMatchContext`/`injectCtx` consistent between option and `serve`.

Note: `wildcardEntry` and `injectCtx` are introduced in Task 3 (not Task 2) specifically so no struct field is written-but-never-read in an intermediate commit, keeping each task's `just lint` (staticcheck `unused`) green.
