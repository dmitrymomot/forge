# Web-transport middleware bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the P3 web-transport keystone — `middleware`, `problem`, `recoverer`, `requestid`, `reqlog`, `clientip` — that turns forge's HTTP seed into a production-usable stack.

**Architecture:** `middleware` defines a `func(http.Handler) http.Handler` seam plus a status/size-capturing `ResponseWriter`. `problem` is a pluggable error-`Responder` seam shipping a JSON (RFC 9457) responder. `clientip` absorbs and hardens the old `request.ClientIP` into a batteries-included resolver + caching middleware + canonical accessor. `recoverer`/`requestid`/`reqlog` are leaf middleware composing those seams. Everything composes externally and is passed to `httpserver.New(handler, ...)`.

**Tech Stack:** Go 1.26, stdlib `net/http`/`net/netip`/`log/slog`, existing forge seed packages (`render`, `errorsx`, `request`, `validate`, `id`, `ctxkey`, `logger`), testify for tests. No new external dependency.

**Design doc:** `docs/superpowers/specs/2026-07-03-web-middleware-bundle-design.md`.

## Global Constraints

- **Module path:** `github.com/dmitrymomot/forge`. Go **1.26**.
- **Work only in the current branch** (`claude/admiring-hermann-4e43a5`); never switch branches.
- **Flat packages** at repo root — one directory per package, no nesting without reason.
- **Options, never builders.** `type Option func(*config)`; construct with `New(...Option)` or a free func returning the value.
- **Black-box tests only** — every test file is `package <pkg>_test` and imports the package under test. Use `github.com/stretchr/testify/assert` (and `require` where a failure must halt the test).
- **Package anatomy:** `doc.go` (runnable example), `options.go`/`config` where configured, `errors.go` for `errors.Is`-matchable single-line sentinels, impl files split by responsibility.
- **Errors:** single-line, `errors.Is`-matchable sentinels; never embed stacks/blobs in the message.
- **Minimal deps:** stdlib + existing seed packages only. No new module in `go.mod`.
- **After each file change:** run `just fmt ./<pkg>/` (runs gofmt, goimports `-local github.com/dmitrymomot/forge`, and `betteralign -apply` which auto-orders struct fields — do not hand-tune field order).
- **Per task:** `just test ./<pkg>/` must pass with `-race` (the recipe adds `-race -cover`). **After the last task:** `just check` (fmt + lint + test) must be green. `just lint` runs `go vet`, `go build`, `golangci-lint`, `nilaway`, `betteralign`, `modernize`.
- **modernize lint:** prefer `for i := range n`, `strings.SplitSeq`, `slices.Backward/Values`, `new(v)` (no `ptr.To` wrapper) — the linter will flag older idioms.
- **No Claude attribution** in any commit message.

---

## File structure

```
middleware/
  middleware.go   Middleware type, Chain, Wrap
  writer.go       ResponseWriter interface, WrapWriter, recorder
  doc.go
  middleware_test.go
  writer_test.go
problem/
  problem.go      Problem struct, config, Option set, From, field/status mapping
  json.go         JSON responder
  errors.go       (none needed; problem classifies, does not mint errors) — omit
  doc.go
  problem_test.go
  json_test.go
clientip/
  clientip.go     resolution engine, Resolve, chain/parse helpers
  options.go      strategy options + config
  presets.go      provider/topology presets
  private.go      privateRanges, PrivateRanges, isPrivate
  middleware.go   Middleware, Get, From, LogExtractor, ctx key
  doc.go
  resolve_test.go
  middleware_test.go
requestid/
  requestid.go    New, config/Option, From, LogExtractor, inbound guard
  doc.go
  requestid_test.go
reqlog/
  reqlog.go       New, config/Option, default level func
  doc.go
  reqlog_test.go
recoverer/
  recoverer.go    New, config/Option, ErrPanic
  errors.go       ErrPanic sentinel
  doc.go
  recoverer_test.go
examples/webmiddleware/
  main.go         end-to-end wiring recipe
request/
  clientip.go       DELETE (moved to clientip)
  clientip_test.go  DELETE (moved to clientip)
  doc.go            MODIFY (drop the ClientIP mention)
```

---

### Task 1: `middleware` — Chain & Wrap

**Files:**
- Create: `middleware/middleware.go`
- Test: `middleware/middleware_test.go`

**Interfaces:**
- Produces: `type Middleware func(http.Handler) http.Handler`; `func Chain(mws ...Middleware) Middleware`; `func Wrap(h http.Handler, mws ...Middleware) http.Handler`.

- [ ] **Step 1: Write the failing test**

`middleware/middleware_test.go`:
```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/middleware"
)

func tag(s string, order *[]string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, s+":in")
			next.ServeHTTP(w, r)
			*order = append(*order, s+":out")
		})
	}
}

func TestWrapOrderOutermostFirst(t *testing.T) {
	var order []string
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
		tag("a", &order), tag("b", &order),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, []string{"a:in", "b:in", "handler", "b:out", "a:out"}, order)
}

func TestWrapEmptyIsIdentity(t *testing.T) {
	called := false
	base := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	middleware.Wrap(base).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, called)
}

func TestChainEqualsWrap(t *testing.T) {
	var order []string
	mw := middleware.Chain(tag("x", &order), tag("y", &order))
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "h") })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, []string{"x:in", "y:in", "h", "y:out", "x:out"}, order)
}
```

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./middleware/`
Expected: FAIL — `undefined: middleware.Wrap` (package does not compile).

- [ ] **Step 3: Write the implementation**

`middleware/middleware.go`:
```go
// Package middleware defines the composition seam every forge HTTP middleware
// implements, plus a response writer that records status and size.
package middleware

import "net/http"

// Middleware wraps an http.Handler with additional behavior. The FIRST Middleware
// passed to Chain/Wrap is the OUTERMOST layer: it sees the request first and the
// response last.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares into one. Chain(a, b, c) applied to h yields
// a(b(c(h))). An empty Chain is the identity wrapper.
func Chain(mws ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}

// Wrap applies mws to h, outermost first. Wrap(h) returns h unchanged.
func Wrap(h http.Handler, mws ...Middleware) http.Handler {
	return Chain(mws...)(h)
}
```

- [ ] **Step 4: Run the test — verify it passes**

Run: `just fmt ./middleware/ && just test ./middleware/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add middleware/middleware.go middleware/middleware_test.go
git commit -m "feat(middleware): Middleware seam with Chain and Wrap"
```

---

### Task 2: `middleware` — ResponseWriter & WrapWriter

**Files:**
- Create: `middleware/writer.go`, `middleware/doc.go`
- Test: `middleware/writer_test.go`

**Interfaces:**
- Produces: `type ResponseWriter interface { http.ResponseWriter; Status() int; Written() int64; Wrote() bool; Unwrap() http.ResponseWriter }`; `func WrapWriter(w http.ResponseWriter) ResponseWriter`.
- Consumes: nothing.

- [ ] **Step 1: Write the failing test**

`middleware/writer_test.go`:
```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/middleware"
)

func TestWrapWriterCapturesStatusAndBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := middleware.WrapWriter(rec)
	assert.False(t, rw.Wrote())
	assert.Equal(t, 0, rw.Status())

	rw.WriteHeader(http.StatusTeapot)
	n, err := rw.Write([]byte("hello"))
	require.NoError(t, err)

	assert.Equal(t, 5, n)
	assert.True(t, rw.Wrote())
	assert.Equal(t, http.StatusTeapot, rw.Status())
	assert.Equal(t, int64(5), rw.Written())
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

func TestWrapWriterImplicit200(t *testing.T) {
	rw := middleware.WrapWriter(httptest.NewRecorder())
	_, _ = rw.Write([]byte("x"))
	assert.Equal(t, http.StatusOK, rw.Status())
}

func TestWrapWriterIdempotent(t *testing.T) {
	rw := middleware.WrapWriter(httptest.NewRecorder())
	assert.Same(t, rw, middleware.WrapWriter(rw))
}

func TestWrapWriterUnwrapReachesFlusher(t *testing.T) {
	rec := httptest.NewRecorder() // implements http.Flusher
	rw := middleware.WrapWriter(rec)
	// http.ResponseController uses Unwrap to reach the underlying Flusher.
	require.NoError(t, http.NewResponseController(rw).Flush())
	assert.True(t, rec.Flushed)
}

func TestWriteHeaderOnlyOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := middleware.WrapWriter(rec)
	rw.WriteHeader(http.StatusCreated)
	rw.WriteHeader(http.StatusBadRequest) // ignored
	assert.Equal(t, http.StatusCreated, rw.Status())
}
```

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./middleware/`
Expected: FAIL — `undefined: middleware.WrapWriter`.

- [ ] **Step 3: Write the implementation**

`middleware/writer.go`:
```go
package middleware

import "net/http"

// ResponseWriter records the status code and body size written through it, and
// whether the header has been committed. It exposes the wrapped writer via Unwrap
// so http.ResponseController can reach optional interfaces (Flusher, Hijacker,
// SetWriteDeadline, ...) without this type re-declaring each one.
type ResponseWriter interface {
	http.ResponseWriter
	Status() int    // 0 until the first write; the WriteHeader code, or 200 on implicit write
	Written() int64 // body bytes written
	Wrote() bool    // has the header been committed?
	Unwrap() http.ResponseWriter
}

type recorder struct {
	http.ResponseWriter
	written int64
	status  int
	wrote   bool
}

// WrapWriter wraps w. If w is already a middleware.ResponseWriter it is returned
// unchanged, so re-wrapping in nested middleware is cheap and non-duplicating.
func WrapWriter(w http.ResponseWriter) ResponseWriter {
	if rw, ok := w.(ResponseWriter); ok {
		return rw
	}
	return &recorder{ResponseWriter: w}
}

func (r *recorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.status = status
	r.wrote = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

func (r *recorder) Status() int {
	if !r.wrote {
		return 0
	}
	return r.status
}

func (r *recorder) Written() int64            { return r.written }
func (r *recorder) Wrote() bool               { return r.wrote }
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
```

`middleware/doc.go`:
```go
// Package middleware defines forge's HTTP composition seam.
//
// A Middleware is a func(http.Handler) http.Handler. Compose middleware with
// Chain/Wrap and pass the result to httpserver.New — the first middleware is the
// outermost layer.
//
//	h := middleware.Wrap(mux,
//		recoverer.New(),
//		requestid.New(),
//	)
//	srv := httpserver.New(h)
//
// WrapWriter records the response status and byte count for access logging and
// panic recovery; use http.ResponseController for flushing/hijacking.
package middleware
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./middleware/ && just test ./middleware/`
Expected: PASS. (fmt's betteralign may reorder `recorder` fields — that is expected.)

- [ ] **Step 5: Lint & commit**

```bash
just lint
git add middleware/writer.go middleware/writer_test.go middleware/doc.go
git commit -m "feat(middleware): status/size-capturing ResponseWriter via Unwrap"
```

---

### Task 3: `problem` — Problem document, options & From

**Files:**
- Create: `problem/problem.go`
- Test: `problem/problem_test.go`

**Interfaces:**
- Consumes: `request.StatusCode(error) int`, `*request.Error` (`.Source`, `.Key string`, `.Kind` with `.String()`); `errorsx.Code(error) (string, bool)`; `validate.Errors` (`.ByField() map[string][]validate.Violation`), `validate.Violation.String()`.
- Produces: `type Problem struct{...}`; `type Option func(*config)`; `func WithLogger(*slog.Logger) Option`; `func WithStatusOf(func(error) int) Option`; `func WithStatus(int) Option`; `func WithTypeBaseURI(string) Option`; `func From(err error, opts ...Option) Problem`. Internal: `newConfig(...Option) config`, `(config).build(err error, r *http.Request) Problem`.

- [ ] **Step 1: Write the failing test**

`problem/problem_test.go`:
```go
package problem_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/errorsx"
	"github.com/dmitrymomot/forge/problem"
	"github.com/dmitrymomot/forge/validate"
)

func TestFromPlainErrorIs400(t *testing.T) {
	p := problem.From(errors.New("boom"))
	assert.Equal(t, http.StatusBadRequest, p.Status)
	assert.Equal(t, "Bad Request", p.Title)
	assert.Equal(t, "about:blank", p.Type)
	assert.Equal(t, "boom", p.Detail)
}

func TestFromNilIs200(t *testing.T) {
	assert.Equal(t, http.StatusOK, problem.From(nil).Status)
}

func TestFromCodedError(t *testing.T) {
	err := errorsx.New("user_not_found", "no such user")
	p := problem.From(err)
	assert.Equal(t, "user_not_found", p.Code)
}

func TestFromValidateErrorsPopulatesFields(t *testing.T) {
	verr := validate.Check(validate.Result{
		{Field: "email", Key: "required", Message: "is required"},
	})
	p := problem.From(verr)
	assert.Equal(t, "is required", p.Fields["email"])
}

func TestForceStatusAndTypeBaseURI(t *testing.T) {
	err := errorsx.New("rate_limited", "slow down")
	p := problem.From(err,
		problem.WithStatus(http.StatusTooManyRequests),
		problem.WithTypeBaseURI("https://errors.example/"),
	)
	assert.Equal(t, http.StatusTooManyRequests, p.Status)
	assert.Equal(t, "https://errors.example/rate_limited", p.Type)
}

func TestFrom5xxHasNoDetail(t *testing.T) {
	p := problem.From(errors.New("db exploded"), problem.WithStatus(http.StatusInternalServerError))
	assert.Empty(t, p.Detail) // never leak internals on 5xx
}
```

> Note: confirm the `validate.Check`/`validate.Result`/`validate.Violation` field names against `validate/validate.go` and `validate/violation.go` while writing — `Violation` has `Field`, `Key`, `Message`. If `validate.Check`'s exact constructor differs, build a `validate.Errors` value directly (it is `[]validate.Violation`).

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./problem/`
Expected: FAIL — `undefined: problem.From`.

- [ ] **Step 3: Write the implementation**

`problem/problem.go`:
```go
// Package problem maps Go errors to HTTP error responses via a pluggable
// Responder seam, and to RFC 9457 problem documents.
package problem

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/errorsx"
	"github.com/dmitrymomot/forge/request"
	"github.com/dmitrymomot/forge/validate"
)

// Problem is an RFC 9457 problem document with forge extensions (Code, Fields).
type Problem struct {
	Type     string            `json:"type,omitempty"`
	Title    string            `json:"title,omitempty"`
	Detail   string            `json:"detail,omitempty"`
	Instance string            `json:"instance,omitempty"`
	Code     string            `json:"code,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
	Status   int               `json:"status"`
}

// Responder writes err as an HTTP error response. It is the seam every
// error-writing middleware/handler accepts.
type Responder func(w http.ResponseWriter, r *http.Request, err error)

type config struct {
	statusOf    func(error) int
	logger      *slog.Logger
	typeBaseURI string
	forceStatus int
}

// Option configures From and the predefined responders.
type Option func(*config)

// WithLogger makes a responder log 5xx errors (with the request context, so
// request_id/client_ip ride along). The body still never contains the error text.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// WithStatusOf overrides the error->status resolver (default request.StatusCode).
func WithStatusOf(fn func(error) int) Option {
	return func(c *config) {
		if fn != nil {
			c.statusOf = fn
		}
	}
}

// WithStatus forces a specific status regardless of the error.
func WithStatus(code int) Option { return func(c *config) { c.forceStatus = code } }

// WithTypeBaseURI sets a base URI prepended to the error Code to form Problem.Type.
func WithTypeBaseURI(uri string) Option { return func(c *config) { c.typeBaseURI = uri } }

func newConfig(opts ...Option) config {
	c := config{statusOf: request.StatusCode}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (c config) status(err error) int {
	if c.forceStatus != 0 {
		return c.forceStatus
	}
	return c.statusOf(err)
}

// build assembles the Problem. r may be nil (From without a request).
func (c config) build(err error, r *http.Request) Problem {
	status := c.status(err)
	p := Problem{
		Status: status,
		Title:  http.StatusText(status),
		Type:   "about:blank",
	}
	if code, ok := errorsx.Code(err); ok {
		p.Code = code
		if c.typeBaseURI != "" {
			p.Type = c.typeBaseURI + code
		}
	}
	if fields := extractFields(err); len(fields) > 0 {
		p.Fields = fields
	}
	if status < 500 && err != nil {
		p.Detail = err.Error()
	}
	if r != nil {
		p.Instance = r.URL.Path
	}
	return p
}

// From maps err to a Problem document without writing a response.
func From(err error, opts ...Option) Problem {
	return newConfig(opts...).build(err, nil)
}

// extractFields pulls per-field messages from a validate.Errors or *request.Error.
func extractFields(err error) map[string]string {
	var ve validate.Errors
	if errors.As(err, &ve) {
		out := make(map[string]string, len(ve))
		for field, vs := range ve.ByField() {
			parts := make([]string, len(vs))
			for i, v := range vs {
				parts[i] = v.String()
			}
			out[field] = strings.Join(parts, "; ")
		}
		return out
	}
	var re *request.Error
	if errors.As(err, &re) {
		key := string(re.Source)
		if re.Key != "" {
			key = re.Key
		}
		return map[string]string{key: re.Kind.String()}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./problem/ && just test ./problem/`
Expected: PASS. If `validate.Check`/`validate.Result` names differ, adjust the test to build `validate.Errors{ {Field:"email", Message:"is required"} }` directly.

- [ ] **Step 5: Commit**

```bash
git add problem/problem.go problem/problem_test.go
git commit -m "feat(problem): RFC 9457 Problem document with error/status/field mapping"
```

---

### Task 4: `problem` — JSON responder

**Files:**
- Create: `problem/json.go`, `problem/doc.go`
- Test: `problem/json_test.go`

**Interfaces:**
- Consumes: `(config).build`, `render.JSON(w, status, v) error`.
- Produces: `func JSON(opts ...Option) Responder`.

- [ ] **Step 1: Write the failing test**

`problem/json_test.go`:
```go
package problem_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/problem"
)

func TestJSONResponderContentTypeAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)

	problem.JSON()(rec, req, errors.New("bad input"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

	var p problem.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, http.StatusBadRequest, p.Status)
	assert.Equal(t, "bad input", p.Detail)
	assert.Equal(t, "/widgets/7", p.Instance)
}

func TestJSONResponder5xxOmitsErrorText(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	problem.JSON(problem.WithStatus(http.StatusInternalServerError))(rec, req, errors.New("secret db dsn leaked"))

	assert.NotContains(t, rec.Body.String(), "secret db dsn")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
```

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./problem/`
Expected: FAIL — `undefined: problem.JSON`.

- [ ] **Step 3: Write the implementation**

`problem/json.go`:
```go
package problem

import (
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/render"
)

// JSON returns a Responder that writes err as application/problem+json (RFC 9457).
// When configured WithLogger, 5xx errors are logged (never placed in the body).
func JSON(opts ...Option) Responder {
	c := newConfig(opts...)
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p := c.build(err, r)
		if p.Status >= 500 && c.logger != nil {
			c.logger.LogAttrs(r.Context(), slog.LevelError, "request error",
				slog.Int("status", p.Status),
				slog.String("error", err.Error()),
			)
		}
		// Set the content type first; render.JSON preserves a preset content type.
		w.Header().Set("Content-Type", "application/problem+json")
		_ = render.JSON(w, p.Status, p)
	}
}
```

`problem/doc.go`:
```go
// Package problem maps errors to HTTP error responses.
//
// The Responder seam is what error-writing middleware accepts:
//
//	type Responder func(w http.ResponseWriter, r *http.Request, err error)
//
// JSON is the shipped responder; it emits RFC 9457 application/problem+json and
// never leaks the error text on 5xx. Consumers wanting HTML supply their own
// Responder. From maps an error to a Problem document without writing.
//
//	recoverer.New(recoverer.WithResponder(problem.JSON(problem.WithLogger(log))))
package problem
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./problem/ && just test ./problem/`
Expected: PASS.

- [ ] **Step 5: Lint & commit**

```bash
just lint
git add problem/json.go problem/doc.go problem/json_test.go
git commit -m "feat(problem): JSON (application/problem+json) responder"
```

---

### Task 5: `clientip` — resolution engine, Resolve & options (+ remove request.ClientIP)

**Files:**
- Create: `clientip/clientip.go`, `clientip/options.go`, `clientip/private.go`
- Test: `clientip/resolve_test.go`
- Delete: `request/clientip.go`, `request/clientip_test.go`
- Modify: `request/doc.go` (drop the `ClientIP` mention)

**Interfaces:**
- Produces: `func Resolve(r *http.Request, opts ...Option) string`; `type Option func(*config)`; strategy options `RemoteAddrOnly`, `SingleHeader(name string)`, `TrustedRanges(cidrs ...string)`, `TrustedHopCount(n int)`, `LeftmostNonPrivate`; `func PrivateRanges() []netip.Prefix`. Internal: `type config`, `newConfig`, `(config).resolve`, `parseAddr`, `remoteHost`, `buildChain`, `isPrivate`.
- Consumes: nothing from the bundle (independent of `middleware`/`problem`).

- [ ] **Step 1: Remove the old resolver so the new package is the only owner**

```bash
git rm request/clientip.go request/clientip_test.go
```
Then edit `request/doc.go`: change the reader list that reads `(BearerToken, ClientIP, presence` to `(BearerToken, presence` (remove `ClientIP, `). Verify nothing else in `request/` references the removed identifiers:
```bash
grep -rn -E 'ClientIP|clientIPTrusted|WithTrustedProxies|WithClientIPHeaders|validIP|remoteHost' request/
```
Expected: no matches (helpers were used only by the deleted file — verified during planning).

- [ ] **Step 2: Write the failing test**

`clientip/resolve_test.go`:
```go
package clientip_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/clientip"
)

func req(remote string, headers map[string][]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for k, vs := range headers {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	return r
}

func TestResolveDefaultIsRemoteAddr(t *testing.T) {
	r := req("203.0.113.9:5555", map[string][]string{
		"X-Forwarded-For": {"1.1.1.1"}, // ignored by the safe default
	})
	assert.Equal(t, "203.0.113.9", clientip.Resolve(r))
}

func TestResolveSingleHeaderStripsPort(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}})
	assert.Equal(t, "198.51.100.7", clientip.Resolve(r, clientip.SingleHeader("CF-Connecting-IP")))
}

func TestResolveSingleHeaderAbsentFallsBackToRemote(t *testing.T) {
	r := req("192.0.2.5:9", nil)
	assert.Equal(t, "192.0.2.5", clientip.Resolve(r, clientip.SingleHeader("CF-Connecting-IP")))
}

func TestResolveTrustedRangesRightmostUntrusted(t *testing.T) {
	// client -> edge(203.0.113.5) -> our proxy(10.0.0.2) -> app
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"203.0.113.5, 10.0.0.2"},
	})
	got := clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8"))
	assert.Equal(t, "203.0.113.5", got)
}

func TestResolveTrustedRangesParsesForwarded(t *testing.T) {
	// The old request.ClientIP ignored Forwarded in trusted mode; now it is honored.
	r := req("10.0.0.2:80", map[string][]string{
		"Forwarded": {`for=203.0.113.9;proto=https`},
	})
	got := clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8"))
	assert.Equal(t, "203.0.113.9", got)
}

func TestResolveMultipleXFFHeaderLinesFlattened(t *testing.T) {
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"203.0.113.5", "10.0.0.9"}, // two separate header lines
	})
	got := clientip.Resolve(r, clientip.TrustedRanges("10.0.0.0/8"))
	assert.Equal(t, "203.0.113.5", got)
}

func TestResolveTrustedHopCount(t *testing.T) {
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"203.0.113.5, 70.70.70.70"},
	})
	// chain valid = [203.0.113.5, 70.70.70.70, 10.0.0.2]; skip 2 from right -> 203.0.113.5
	assert.Equal(t, "203.0.113.5", clientip.Resolve(r, clientip.TrustedHopCount(2)))
}

func TestResolveLeftmostNonPrivate(t *testing.T) {
	r := req("10.0.0.2:80", map[string][]string{
		"X-Forwarded-For": {"10.9.9.9, 203.0.113.5, 10.0.0.2"},
	})
	assert.Equal(t, "203.0.113.5", clientip.Resolve(r, clientip.LeftmostNonPrivate()))
}

func TestResolveIPv6MappedNormalized(t *testing.T) {
	r := req("[::ffff:192.0.2.7]:9", nil)
	assert.Equal(t, "192.0.2.7", clientip.Resolve(r))
}

func TestResolveMalformedRemoteAddr(t *testing.T) {
	assert.Equal(t, "", clientip.Resolve(req("garbage", nil)))
}

func TestTrustedRangesPanicsOnBadCIDR(t *testing.T) {
	assert.Panics(t, func() { clientip.TrustedRanges("not-a-cidr") })
}
```

- [ ] **Step 3: Run the test — verify it fails**

Run: `just test ./clientip/`
Expected: FAIL — package `clientip` does not exist.

- [ ] **Step 4: Write the private-ranges file**

`clientip/private.go`:
```go
package clientip

import (
	"net/netip"
	"slices"
)

// privateRanges are the ranges TrustPrivateProxies trusts and LeftmostNonPrivate
// skips: RFC 1918, loopback, link-local, CGNAT (RFC 6598), IPv6 loopback,
// link-local, and ULA (RFC 4193).
var privateRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
}

// PrivateRanges returns a copy of the private/loopback/link-local/CGNAT/ULA
// prefixes, for composing a custom TrustedRanges.
func PrivateRanges() []netip.Prefix { return slices.Clone(privateRanges) }

func inPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func isPrivate(addr netip.Addr) bool { return inPrefixes(addr, privateRanges) }
```

- [ ] **Step 5: Write the options file**

`clientip/options.go`:
```go
package clientip

import (
	"net/netip"
	"strconv"
)

type mode int

const (
	modeRemoteAddr mode = iota
	modeSingleHeader
	modeTrustedRanges
	modeTrustedHopCount
	modeLeftmostNonPrivate
)

type config struct {
	header   string
	trusted  []netip.Prefix
	hopCount int
	mode     mode
}

// Option configures client-IP resolution. Strategy options are last-wins.
type Option func(*config)

func newConfig(opts ...Option) config {
	c := config{mode: modeRemoteAddr}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// RemoteAddrOnly ignores all headers and uses RemoteAddr. This is the default.
func RemoteAddrOnly() Option { return func(c *config) { c.mode = modeRemoteAddr } }

// SingleHeader trusts one edge header (first valid IP, port stripped), falling
// back to RemoteAddr when the header is absent.
func SingleHeader(name string) Option {
	return func(c *config) { c.mode = modeSingleHeader; c.header = name }
}

// TrustedRanges enables spoof-resistant resolution: the XFF+Forwarded chain is
// walked right-to-left and the first address outside the trusted CIDRs is
// returned. It PANICS on an invalid CIDR string — trusted-proxy ranges are static
// boot config for a security control, so a typo must fail loudly at startup
// rather than silently mis-scope trust.
func TrustedRanges(cidrs ...string) Option {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			panic("clientip: TrustedRanges: invalid CIDR " + strconv.Quote(s) + ": " + err.Error())
		}
		prefixes = append(prefixes, p)
	}
	return func(c *config) { c.mode = modeTrustedRanges; c.trusted = prefixes }
}

// TrustedHopCount returns the address n hops from the right of the XFF+Forwarded
// chain (n = number of trusted proxies in front of the app).
func TrustedHopCount(n int) Option {
	return func(c *config) { c.mode = modeTrustedHopCount; c.hopCount = n }
}

// LeftmostNonPrivate returns the leftmost public address in the chain. Best-effort
// and spoofable — for logging only, never for auth/rate-limiting.
func LeftmostNonPrivate() Option { return func(c *config) { c.mode = modeLeftmostNonPrivate } }
```

- [ ] **Step 6: Write the engine**

`clientip/clientip.go`:
```go
// Package clientip resolves the originating client IP from an HTTP request,
// caches it per request, and exposes it to handlers, other middleware, and the
// logger. It replaces the former request.ClientIP with a safe-by-default,
// batteries-included resolver.
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// Resolve returns the client IP for r per opts, or "" if nothing parses. With no
// options it is safe-by-default: RemoteAddr only. Header-trusting strategies must
// be requested explicitly (SingleHeader, TrustedRanges, a provider preset, ...).
func Resolve(r *http.Request, opts ...Option) string {
	return newConfig(opts...).resolve(r)
}

func (c config) resolve(r *http.Request) string {
	switch c.mode {
	case modeSingleHeader:
		if ip := firstHeaderIP(r, c.header); ip != "" {
			return ip
		}
		return remoteHost(r.RemoteAddr)
	case modeTrustedRanges:
		return rightmostUntrusted(buildChain(r), c.trusted)
	case modeTrustedHopCount:
		return nthFromRight(buildChain(r), c.hopCount, r)
	case modeLeftmostNonPrivate:
		return leftmostNonPrivate(buildChain(r), r)
	default: // modeRemoteAddr
		return remoteHost(r.RemoteAddr)
	}
}

// buildChain returns the ordered forwarding chain: every X-Forwarded-For entry
// (across repeated header lines), then every RFC 7239 Forwarded "for=" entry,
// then RemoteAddr. Left is closest to the client; right is the nearest proxy.
func buildChain(r *http.Request) []string {
	var chain []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		for part := range strings.SplitSeq(line, ",") {
			chain = append(chain, part)
		}
	}
	for _, line := range r.Header.Values("Forwarded") {
		chain = append(chain, forwardedFors(line)...)
	}
	return append(chain, r.RemoteAddr)
}

// forwardedFors extracts every for= value from one RFC 7239 Forwarded header line.
func forwardedFors(v string) []string {
	var out []string
	for elem := range strings.SplitSeq(v, ",") {
		for kv := range strings.SplitSeq(elem, ";") {
			key, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
				continue
			}
			out = append(out, strings.Trim(strings.TrimSpace(val), `"`))
		}
	}
	return out
}

func firstHeaderIP(r *http.Request, name string) string {
	for _, line := range r.Header.Values(name) {
		for part := range strings.SplitSeq(line, ",") {
			if addr, ok := parseAddr(part); ok {
				return addr.String()
			}
		}
	}
	return ""
}

func rightmostUntrusted(chain []string, trusted []netip.Prefix) string {
	for hop := range slices.Backward(chain) {
		addr, ok := parseAddr(hop)
		if !ok || inPrefixes(addr, trusted) {
			continue
		}
		return addr.String()
	}
	// Every hop trusted/unparsable: leftmost non-private, else RemoteAddr (last hop).
	for _, hop := range chain {
		if addr, ok := parseAddr(hop); ok && !isPrivate(addr) {
			return addr.String()
		}
	}
	if len(chain) > 0 {
		if addr, ok := parseAddr(chain[len(chain)-1]); ok {
			return addr.String()
		}
	}
	return ""
}

func nthFromRight(chain []string, n int, r *http.Request) string {
	valid := make([]netip.Addr, 0, len(chain))
	for _, hop := range chain {
		if addr, ok := parseAddr(hop); ok {
			valid = append(valid, addr)
		}
	}
	idx := len(valid) - 1 - n
	if idx < 0 || idx >= len(valid) {
		return remoteHost(r.RemoteAddr)
	}
	return valid[idx].String()
}

func leftmostNonPrivate(chain []string, r *http.Request) string {
	for _, hop := range chain {
		if addr, ok := parseAddr(hop); ok && !isPrivate(addr) {
			return addr.String()
		}
	}
	return remoteHost(r.RemoteAddr)
}

// parseAddr normalizes a chain token (which may carry a port or brackets) to a
// netip.Addr, unmapping IPv4-in-IPv6. ok is false when it is not a valid address.
func parseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.Trim(s, "[]")
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// remoteHost returns the bare IP from a RemoteAddr ("ip:port" or "ip"), or "".
func remoteHost(addr string) string {
	if a, ok := parseAddr(addr); ok {
		return a.String()
	}
	return ""
}
```

- [ ] **Step 7: Run the tests — verify they pass**

Run: `just fmt ./clientip/ ./request/ && just test ./clientip/ ./request/`
Expected: PASS for both packages (request still compiles and its remaining tests pass after the deletion).

- [ ] **Step 8: Commit**

```bash
git add clientip/clientip.go clientip/options.go clientip/private.go clientip/resolve_test.go request/doc.go
git rm --cached request/clientip.go request/clientip_test.go 2>/dev/null || true
git commit -m "feat(clientip): hardened client-IP engine; remove request.ClientIP

Multi-header XFF reads, RFC 7239 Forwarded in trusted mode, strategy
options, safe RemoteAddr-only default. Relocates request.ClientIP."
```

---

### Task 6: `clientip` — provider & topology presets

**Files:**
- Create: `clientip/presets.go`
- Test: add to `clientip/resolve_test.go`

**Interfaces:**
- Produces: `Cloudflare`, `Fastly`, `CloudFront`, `Akamai`, `AzureFrontDoor`, `Envoy`, `XRealIP`, `TrustPrivateProxies` — each `func() Option`.

- [ ] **Step 1: Write the failing test** (append to `clientip/resolve_test.go`)

```go
func TestCloudflarePreset(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"CF-Connecting-IP": {"198.51.100.7"}})
	assert.Equal(t, "198.51.100.7", clientip.Resolve(r, clientip.Cloudflare()))
}

func TestEnvoyPreset(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"x-envoy-external-address": {"203.0.113.4"}})
	assert.Equal(t, "203.0.113.4", clientip.Resolve(r, clientip.Envoy()))
}

func TestCloudFrontStripsPort(t *testing.T) {
	r := req("10.0.0.1:1", map[string][]string{"CloudFront-Viewer-Address": {"203.0.113.8:52193"}})
	assert.Equal(t, "203.0.113.8", clientip.Resolve(r, clientip.CloudFront()))
}

func TestTrustPrivateProxies(t *testing.T) {
	r := req("10.1.1.1:80", map[string][]string{"X-Forwarded-For": {"203.0.113.5, 10.1.1.1"}})
	assert.Equal(t, "203.0.113.5", clientip.Resolve(r, clientip.TrustPrivateProxies()))
}
```

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./clientip/`
Expected: FAIL — `undefined: clientip.Cloudflare`.

- [ ] **Step 3: Write the implementation**

`clientip/presets.go`:
```go
package clientip

// Provider presets exist ONLY for edges that set a dedicated, reliable header
// they overwrite and strip on ingress. Generic XFF proxies (nginx, Caddy,
// Traefik, HAProxy, k8s ingress, cloud LBs) have no guaranteed dedicated header —
// use TrustPrivateProxies, TrustedRanges, TrustedHopCount, or XRealIP instead.

// Cloudflare trusts the CF-Connecting-IP header.
func Cloudflare() Option { return SingleHeader("CF-Connecting-IP") }

// Fastly trusts the Fastly-Client-IP header.
func Fastly() Option { return SingleHeader("Fastly-Client-IP") }

// CloudFront trusts the CloudFront-Viewer-Address header (its port is stripped).
func CloudFront() Option { return SingleHeader("CloudFront-Viewer-Address") }

// Akamai trusts the True-Client-IP header.
func Akamai() Option { return SingleHeader("True-Client-IP") }

// AzureFrontDoor trusts the X-Azure-ClientIP header.
func AzureFrontDoor() Option { return SingleHeader("X-Azure-ClientIP") }

// Envoy trusts the x-envoy-external-address header (Envoy's computed trusted
// client address).
func Envoy() Option { return SingleHeader("x-envoy-external-address") }

// XRealIP trusts the X-Real-IP header — the de-facto header nginx/Traefik/ingress
// set when configured. Only safe when your proxy always overwrites it.
func XRealIP() Option { return SingleHeader("X-Real-IP") }

// TrustPrivateProxies trusts all private/loopback/link-local/CGNAT/ULA ranges as
// proxies and returns the rightmost untrusted address — the standard setup for an
// app behind a reverse proxy on a private network.
func TrustPrivateProxies() Option {
	return func(c *config) { c.mode = modeTrustedRanges; c.trusted = PrivateRanges() }
}
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./clientip/ && just test ./clientip/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add clientip/presets.go clientip/resolve_test.go
git commit -m "feat(clientip): provider and private-proxy presets"
```

---

### Task 7: `clientip` — caching Middleware, Get/From & LogExtractor

**Files:**
- Create: `clientip/middleware.go`, `clientip/doc.go`
- Test: `clientip/middleware_test.go`

**Interfaces:**
- Consumes: `middleware.Middleware`, `ctxkey.New[string]`, `logger.ContextExtractor`.
- Produces: `func Middleware(opts ...Option) middleware.Middleware`; `func Get(r *http.Request) string`; `func From(ctx context.Context) (string, bool)`; `var LogExtractor logger.ContextExtractor`.

- [ ] **Step 1: Write the failing test**

`clientip/middleware_test.go`:
```go
package clientip_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/clientip"
)

func TestMiddlewareCachesAndGetReads(t *testing.T) {
	var got string
	h := clientip.Middleware(clientip.TrustPrivateProxies())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = clientip.Get(r) }),
	)
	r := req("10.0.0.2:80", map[string][]string{"X-Forwarded-For": {"203.0.113.5, 10.0.0.2"}})
	h.ServeHTTP(httptest.NewRecorder(), r)
	assert.Equal(t, "203.0.113.5", got)
}

func TestFromReportsRanEvenWhenEmpty(t *testing.T) {
	var ip string
	var ok bool
	h := clientip.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, ok = clientip.From(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), req("garbage", nil)) // resolves to ""
	assert.True(t, ok) // middleware ran
	assert.Equal(t, "", ip)
}

func TestGetFallsBackWhenMiddlewareAbsent(t *testing.T) {
	r := req("192.0.2.5:9", nil)
	assert.Equal(t, "192.0.2.5", clientip.Get(r)) // safe RemoteAddr fallback
}

func TestFromAbsentReportsNotRun(t *testing.T) {
	_, ok := clientip.From(context.Background())
	assert.False(t, ok)
}

func TestLogExtractor(t *testing.T) {
	ctx := context.Background()
	_, ok := clientip.LogExtractor(ctx)
	assert.False(t, ok) // no value -> skip

	var captured string
	h := clientip.Middleware(clientip.TrustPrivateProxies())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attr, ok := clientip.LogExtractor(r.Context())
			if ok {
				captured = attr.Value.String()
			}
			assert.Equal(t, slog.String("client_ip", "203.0.113.5"), attr)
		}),
	)
	h.ServeHTTP(httptest.NewRecorder(), req("10.0.0.2:80", map[string][]string{"X-Forwarded-For": {"203.0.113.5, 10.0.0.2"}}))
	assert.Equal(t, "203.0.113.5", captured)
}
```

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./clientip/`
Expected: FAIL — `undefined: clientip.Middleware`.

- [ ] **Step 3: Write the implementation**

`clientip/middleware.go`:
```go
package clientip

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/ctxkey"
	"github.com/dmitrymomot/forge/logger"
	"github.com/dmitrymomot/forge/middleware"
)

var ipKey = ctxkey.New[string]("clientip")

// Middleware resolves the client IP once per request (using opts) and caches it
// in the request context for Get, From, and LogExtractor.
func Middleware(opts ...Option) middleware.Middleware {
	c := newConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := c.resolve(r)
			next.ServeHTTP(w, r.WithContext(ipKey.With(r.Context(), ip)))
		})
	}
}

// From returns the cached client IP. ok reports whether Middleware ran — true
// even when the resolved IP is "" (resolution ran but nothing parsed).
func From(ctx context.Context) (string, bool) { return ipKey.From(ctx) }

// Get returns the client IP: the value cached by Middleware if it ran, else a
// safe RemoteAddr-only fallback. Handlers and other middleware should call Get
// rather than re-parsing headers.
func Get(r *http.Request) string {
	if ip, ok := From(r.Context()); ok {
		return ip
	}
	return Resolve(r)
}

// LogExtractor adds a "client_ip" attribute when Middleware cached a non-empty IP.
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	ip, ok := ipKey.From(ctx)
	if !ok || ip == "" {
		return slog.Attr{}, false
	}
	return slog.String("client_ip", ip), true
}
```

`clientip/doc.go`:
```go
// Package clientip resolves and caches the originating client IP.
//
// Install Middleware once with your topology, then call Get/From everywhere:
//
//	h := clientip.Middleware(clientip.TrustPrivateProxies())(mux)
//	// in a handler:
//	ip := clientip.Get(r)
//
// Wire LogExtractor into the logger so every log line during a request carries
// client_ip:
//
//	logger.New(logger.WithContextExtractors(clientip.LogExtractor))
//
// Strategies: RemoteAddrOnly (default, safe), SingleHeader, TrustedRanges,
// TrustedHopCount, LeftmostNonPrivate. Presets: Cloudflare, Fastly, CloudFront,
// Akamai, AzureFrontDoor, Envoy, XRealIP, TrustPrivateProxies.
package clientip
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./clientip/ && just test ./clientip/`
Expected: PASS.

- [ ] **Step 5: Lint & commit**

```bash
just lint
git add clientip/middleware.go clientip/doc.go clientip/middleware_test.go
git commit -m "feat(clientip): caching Middleware, Get/From accessor, LogExtractor"
```

---

### Task 8: `requestid`

**Files:**
- Create: `requestid/requestid.go`, `requestid/doc.go`
- Test: `requestid/requestid_test.go`

**Interfaces:**
- Consumes: `middleware.Middleware`, `id.NewULID().String()`, `ctxkey.New[string]`, `logger.ContextExtractor`.
- Produces: `func New(opts ...Option) middleware.Middleware`; `WithHeader(string)`, `WithGenerator(func() string)`, `WithTrustInbound(bool)` — each `Option`; `func From(context.Context) (string, bool)`; `var LogExtractor logger.ContextExtractor`.

- [ ] **Step 1: Write the failing test**

`requestid/requestid_test.go`:
```go
package requestid_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/requestid"
)

func serve(mw func(http.Handler) http.Handler, r *http.Request, h http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, r)
	return rec
}

func TestGeneratesWhenAbsent(t *testing.T) {
	var seen string
	rec := serve(requestid.New(), httptest.NewRequest(http.MethodGet, "/", nil),
		func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotEmpty(t, seen)
	assert.Equal(t, seen, rec.Header().Get("X-Request-ID"))
}

func TestTrustsValidInbound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	var seen string
	serve(requestid.New(), req, func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.Equal(t, "abc-123", seen)
}

func TestRejectsOversizedInbound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("a", 129))
	var seen string
	serve(requestid.New(), req, func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotEqual(t, strings.Repeat("a", 129), seen)
	assert.NotEmpty(t, seen) // generated instead
}

func TestRejectsNonASCIIInbound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "id\nwith\rnewline")
	var seen string
	serve(requestid.New(), req, func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotContains(t, seen, "\n")
}

func TestTrustInboundDisabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "client-supplied")
	var seen string
	serve(requestid.New(requestid.WithTrustInbound(false)), req,
		func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) })
	assert.NotEqual(t, "client-supplied", seen)
}

func TestCustomHeaderAndGenerator(t *testing.T) {
	var seen string
	rec := serve(
		requestid.New(requestid.WithHeader("X-Trace"), requestid.WithGenerator(func() string { return "fixed" })),
		httptest.NewRequest(http.MethodGet, "/", nil),
		func(w http.ResponseWriter, r *http.Request) { seen, _ = requestid.From(r.Context()) },
	)
	assert.Equal(t, "fixed", seen)
	assert.Equal(t, "fixed", rec.Header().Get("X-Trace"))
}

func TestLogExtractor(t *testing.T) {
	var attrOK bool
	serve(requestid.New(requestid.WithGenerator(func() string { return "rid-1" })),
		httptest.NewRequest(http.MethodGet, "/", nil),
		func(w http.ResponseWriter, r *http.Request) {
			attr, ok := requestid.LogExtractor(r.Context())
			attrOK = ok
			assert.Equal(t, "rid-1", attr.Value.String())
		})
	assert.True(t, attrOK)
}
```

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./requestid/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

`requestid/requestid.go`:
```go
// Package requestid attaches a correlation ID to each request: an accepted
// inbound header value or a freshly generated ULID.
package requestid

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/ctxkey"
	"github.com/dmitrymomot/forge/id"
	"github.com/dmitrymomot/forge/logger"
	"github.com/dmitrymomot/forge/middleware"
)

var idKey = ctxkey.New[string]("requestid")

func defaultGenerator() string { return id.NewULID().String() }

type config struct {
	header       string
	generator    func() string
	trustInbound bool
}

// Option configures the requestid middleware.
type Option func(*config)

// WithHeader sets the request/response header name (default "X-Request-ID").
func WithHeader(name string) Option {
	return func(c *config) {
		if name != "" {
			c.header = name
		}
	}
}

// WithGenerator sets the ID generator (default a ULID string).
func WithGenerator(gen func() string) Option {
	return func(c *config) {
		if gen != nil {
			c.generator = gen
		}
	}
}

// WithTrustInbound controls whether a valid inbound header is accepted (default true).
func WithTrustInbound(trust bool) Option { return func(c *config) { c.trustInbound = trust } }

// New returns middleware that stores the request ID in context, echoes it on the
// response header, and exposes it via From and LogExtractor.
func New(opts ...Option) middleware.Middleware {
	c := config{header: "X-Request-ID", generator: defaultGenerator, trustInbound: true}
	for _, o := range opts {
		o(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := ""
			if c.trustInbound {
				if v := r.Header.Get(c.header); validInbound(v) {
					rid = v
				}
			}
			if rid == "" {
				rid = c.generator()
			}
			w.Header().Set(c.header, rid)
			next.ServeHTTP(w, r.WithContext(idKey.With(r.Context(), rid)))
		})
	}
}

// validInbound accepts a non-empty, printable-ASCII value of at most 128 bytes,
// so a client cannot inject control characters into logs or the echoed header.
func validInbound(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// From returns the request ID stored by New.
func From(ctx context.Context) (string, bool) { return idKey.From(ctx) }

// LogExtractor adds a "request_id" attribute when one is present.
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	rid, ok := idKey.From(ctx)
	if !ok || rid == "" {
		return slog.Attr{}, false
	}
	return slog.String("request_id", rid), true
}
```

`requestid/doc.go`:
```go
// Package requestid attaches a per-request correlation ID.
//
//	h := requestid.New()(mux)
//	logger.New(logger.WithContextExtractors(requestid.LogExtractor))
//
// A valid inbound X-Request-ID (printable ASCII, <=128 bytes) is trusted by
// default; otherwise a ULID is generated. The value is echoed on the response and
// available via requestid.From(ctx).
package requestid
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./requestid/ && just test ./requestid/`
Expected: PASS.

- [ ] **Step 5: Lint & commit**

```bash
just lint
git add requestid/requestid.go requestid/doc.go requestid/requestid_test.go
git commit -m "feat(requestid): correlation ID middleware with inbound guard and log extractor"
```

---

### Task 9: `reqlog`

**Files:**
- Create: `reqlog/reqlog.go`, `reqlog/doc.go`
- Test: `reqlog/reqlog_test.go`

**Interfaces:**
- Consumes: `middleware.WrapWriter`, `*slog.Logger`.
- Produces: `func New(log *slog.Logger, opts ...Option) middleware.Middleware`; `WithLevelFunc(func(int) slog.Level)`, `WithSkip(func(*http.Request) bool)` — each `Option`.

- [ ] **Step 1: Write the failing test**

`reqlog/reqlog_test.go`:
```go
package reqlog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/reqlog"
	"github.com/dmitrymomot/forge/requestid"
)

func newLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m))
	return m
}

func TestLogsAccessLine(t *testing.T) {
	log, buf := newLogger()
	h := reqlog.New(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hi"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/things", nil))

	m := lastLine(t, buf)
	assert.Equal(t, "POST", m["method"])
	assert.Equal(t, "/things", m["path"])
	assert.Equal(t, float64(http.StatusCreated), m["status"])
	assert.Equal(t, float64(2), m["bytes"])
}

func TestLevelByStatus(t *testing.T) {
	log, buf := newLogger()
	h := reqlog.New(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, "ERROR", lastLine(t, buf)["level"])
}

func TestSkip(t *testing.T) {
	log, buf := newLogger()
	skip := func(r *http.Request) bool { return r.URL.Path == "/healthz" }
	h := reqlog.New(log, reqlog.WithSkip(skip))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Empty(t, buf.String())
}

func TestInjectsRequestIDViaExtractor(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	// logger.New would wire extractors; here assert reqlog logs with request context
	// so a requestid-populated context surfaces the ID through a context handler.
	log := slog.New(base)

	h := requestid.New(requestid.WithGenerator(func() string { return "rid-9" }))(
		reqlog.New(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// The plain handler has no extractor; this test just asserts reqlog uses
	// r.Context() (no panic) and emits a line. Extractor wiring is covered E2E
	// in the examples build. Assert a line was written:
	assert.Contains(t, buf.String(), `"msg":"http request"`)
}
```

> The `TestInjectsRequestIDViaExtractor` end-to-end extractor assertion is exercised by the examples recipe (Task 11); here we only assert reqlog logs via the request context.

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./reqlog/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

`reqlog/reqlog.go`:
```go
// Package reqlog logs one structured line per HTTP request.
package reqlog

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/middleware"
)

type config struct {
	levelFunc func(status int) slog.Level
	skip      func(*http.Request) bool
}

// Option configures the reqlog middleware.
type Option func(*config)

// WithLevelFunc maps the response status to a log level (default 5xx->Error,
// 4xx->Warn, else Info).
func WithLevelFunc(fn func(status int) slog.Level) Option {
	return func(c *config) {
		if fn != nil {
			c.levelFunc = fn
		}
	}
}

// WithSkip skips logging for requests where pred returns true (e.g. health checks).
func WithSkip(pred func(*http.Request) bool) Option { return func(c *config) { c.skip = pred } }

func defaultLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// New returns middleware that logs method, path, status, duration, and bytes for
// each request, using the request context so wired ContextExtractors (request_id,
// client_ip) are included automatically. A nil log uses slog.Default().
func New(log *slog.Logger, opts ...Option) middleware.Middleware {
	if log == nil {
		log = slog.Default()
	}
	c := config{levelFunc: defaultLevel}
	for _, o := range opts {
		o(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.skip != nil && c.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			rw := middleware.WrapWriter(w)
			start := time.Now()
			next.ServeHTTP(rw, r)
			status := rw.Status()
			if status == 0 {
				status = http.StatusOK
			}
			log.LogAttrs(r.Context(), c.levelFunc(status), "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Duration("duration", time.Since(start)),
				slog.Int64("bytes", rw.Written()),
			)
		})
	}
}
```

`reqlog/doc.go`:
```go
// Package reqlog emits one structured access-log line per request.
//
//	log := logger.New(logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor))
//	h := reqlog.New(log, reqlog.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" }))(mux)
//
// The line carries method, path, status, duration, and bytes; request_id and
// client_ip arrive via the logger's extractors, not reqlog itself.
package reqlog
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./reqlog/ && just test ./reqlog/`
Expected: PASS.

- [ ] **Step 5: Lint & commit**

```bash
just lint
git add reqlog/reqlog.go reqlog/doc.go reqlog/reqlog_test.go
git commit -m "feat(reqlog): structured per-request access log"
```

---

### Task 10: `recoverer`

**Files:**
- Create: `recoverer/recoverer.go`, `recoverer/errors.go`, `recoverer/doc.go`
- Test: `recoverer/recoverer_test.go`

**Interfaces:**
- Consumes: `middleware.WrapWriter`, `problem.Responder`, `problem.JSON()`.
- Produces: `func New(opts ...Option) middleware.Middleware`; `WithResponder(problem.Responder)`, `WithLogger(*slog.Logger)` — each `Option`; `var ErrPanic error`.

- [ ] **Step 1: Write the failing test**

`recoverer/recoverer_test.go`:
```go
package recoverer_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/problem"
	"github.com/dmitrymomot/forge/recoverer"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestPanicBecomes500Problem(t *testing.T) {
	h := recoverer.New(recoverer.WithLogger(discard()))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Body.String(), "boom") // 5xx never leaks the panic value
}

func TestErrPanicIsMatchable(t *testing.T) {
	var captured error
	responder := func(w http.ResponseWriter, r *http.Request, err error) { captured = err }
	h := recoverer.New(recoverer.WithLogger(discard()), recoverer.WithResponder(responder))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") }),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.ErrorIs(t, captured, recoverer.ErrPanic)
}

func TestAlreadyWrittenOnlyLogs(t *testing.T) {
	h := recoverer.New(recoverer.WithLogger(discard()))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))
			panic("late")
		}),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code) // status already committed, unchanged
	assert.Equal(t, "partial", rec.Body.String())
}

func TestAbortHandlerRepanics(t *testing.T) {
	h := recoverer.New(recoverer.WithLogger(discard()))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }),
	)
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

func TestNoPanicPassesThrough(t *testing.T) {
	h := recoverer.New()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

var _ = errors.Is // keep errors imported if unused above
```

- [ ] **Step 2: Run the test — verify it fails**

Run: `just test ./recoverer/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

`recoverer/errors.go`:
```go
package recoverer

import "errors"

// ErrPanic wraps a recovered panic value passed to the Responder, so responders
// and logs can match it with errors.Is.
var ErrPanic = errors.New("recoverer: handler panicked")
```

`recoverer/recoverer.go`:
```go
// Package recoverer converts handler panics into a 500 response and a log line.
package recoverer

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/dmitrymomot/forge/middleware"
	"github.com/dmitrymomot/forge/problem"
)

type config struct {
	responder problem.Responder
	logger    *slog.Logger
}

// Option configures the recoverer middleware.
type Option func(*config)

// WithResponder sets the error responder used to write the 500 (default problem.JSON()).
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

// WithLogger sets the logger for panic reports (default slog.Default()).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// New returns middleware that recovers panics, logs them at Error with the
// request context, and (if nothing was written yet) writes a 500 via the
// responder. http.ErrAbortHandler is re-panicked so net/http can abort silently.
func New(opts ...Option) middleware.Middleware {
	c := config{responder: problem.JSON(), logger: slog.Default()}
	for _, o := range opts {
		o(&c)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := middleware.WrapWriter(w)
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				c.logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)
				if !rw.Wrote() {
					c.responder(rw, r, fmt.Errorf("%w: %v", ErrPanic, v))
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}
```

`recoverer/doc.go`:
```go
// Package recoverer is the outermost middleware: it turns a handler panic into a
// 500 response and an Error log line.
//
//	h := middleware.Wrap(mux, recoverer.New()) // defaults to problem.JSON()
//
// The recovered value is wrapped in ErrPanic and passed to the responder. If the
// handler already committed a response, recoverer only logs. http.ErrAbortHandler
// propagates unchanged.
package recoverer
```

- [ ] **Step 4: Run the tests — verify they pass**

Run: `just fmt ./recoverer/ && just test ./recoverer/`
Expected: PASS. (If `errorlint` flags `v == http.ErrAbortHandler`, keep it — `v` is `any`, not an error value, so `errors.Is` does not apply; add a `//nolint:errorlint` only if the linter actually errors.)

- [ ] **Step 5: Lint & commit**

```bash
just lint
git add recoverer/recoverer.go recoverer/errors.go recoverer/doc.go recoverer/recoverer_test.go
git commit -m "feat(recoverer): panic recovery via problem responder"
```

---

### Task 11: `examples/webmiddleware` end-to-end recipe

**Files:**
- Create: `examples/webmiddleware/main.go`

**Interfaces:**
- Consumes: everything above + `httpserver`, `logger`, `render`, `errorsx`, `validate`.

- [ ] **Step 1: Write the recipe**

`examples/webmiddleware/main.go`:
```go
// Command webmiddleware demonstrates the full web-transport middleware chain:
// recoverer -> requestid -> clientip -> reqlog, with a problem responder and a
// logger wired to the request_id and client_ip extractors.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"

	"github.com/dmitrymomot/forge/clientip"
	"github.com/dmitrymomot/forge/errorsx"
	"github.com/dmitrymomot/forge/httpserver"
	"github.com/dmitrymomot/forge/logger"
	"github.com/dmitrymomot/forge/middleware"
	"github.com/dmitrymomot/forge/problem"
	"github.com/dmitrymomot/forge/reqlog"
	"github.com/dmitrymomot/forge/requestid"
	"github.com/dmitrymomot/forge/recoverer"
	"github.com/dmitrymomot/forge/render"
)

func main() {
	log, err := logger.New(logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor))
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, r *http.Request) {
		_ = render.JSON(w, http.StatusOK, map[string]string{"ip": clientip.Get(r)})
	})
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
		problem.JSON(problem.WithLogger(log))(w, r, errorsx.New("teapot", "I refuse"))
	})
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) {
		panic(errors.New("unexpected"))
	})

	h := middleware.Wrap(mux,
		recoverer.New(recoverer.WithLogger(log)),
		requestid.New(),
		clientip.Middleware(clientip.TrustPrivateProxies()),
		reqlog.New(log, reqlog.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/healthz" })),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := httpserver.New(h, httpserver.WithAddr(":8080"), httpserver.WithLogger(log))
	if err := srv.Run(ctx); err != nil {
		log.Error("server exited", "error", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./examples/webmiddleware/`
Expected: builds with no error. (Adjust import ordering per `just fmt`; note `examples/` is excluded from the modernize linter.)

- [ ] **Step 3: Final full check**

Run: `just check`
Expected: fmt clean, lint clean (vet, build, golangci-lint, nilaway, betteralign, modernize), all tests pass with `-race`.

- [ ] **Step 4: Commit**

```bash
just fmt ./examples/webmiddleware/
git add examples/webmiddleware/main.go
git commit -m "docs(examples): end-to-end web-middleware chain recipe"
```

---

## Self-Review

**1. Spec coverage:**
- `middleware` seam + `WrapWriter` → Tasks 1–2. ✓
- `problem` Responder/Problem/From/JSON, 5xx no-leak, field/code mapping → Tasks 3–4. ✓
- `clientip` engine (multi-header, Forwarded-in-trusted, safe default), strategies, presets, Middleware/Get/From/LogExtractor, request.ClientIP migration → Tasks 5–7. ✓
- `requestid` (inbound guard, generator, extractor) → Task 8. ✓
- `reqlog` (level-by-status, skip, context logging) → Task 9. ✓
- `recoverer` (500 via responder, ErrPanic, ErrAbortHandler re-panic, already-written) → Task 10. ✓
- example recipe → Task 11. ✓
- Breaking change (remove request.ClientIP) → Task 5, verified isolated. ✓

**2. Placeholder scan:** No TBD/TODO; every code step shows full code. The one deferred assertion (reqlog↔extractor end-to-end) is explicitly delegated to the examples build with a reason, and the local reqlog test still asserts real behavior.

**3. Type consistency:** `middleware.Middleware`, `middleware.ResponseWriter`/`WrapWriter`, `problem.Responder`/`Problem`/`From`/`JSON`, `clientip.Option`/`Resolve`/`Middleware`/`Get`/`From`/`LogExtractor`, `requestid.From`/`LogExtractor`, `recoverer.ErrPanic` are used identically across tasks. `id.NewULID().String()` (not `id.New`) is used for generation. `From(ctx)` returns `(string, bool)` consistently in `clientip` and `requestid`.

## Execution Handoff

Two execution options — see below.
