# auth/guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `auth/guard` — request-authentication middleware (Verifier seam + chained extractors + Basic Auth + context Identity) per the approved spec at `docs/superpowers/specs/2026-07-13-auth-guard-design.md`.

**Architecture:** One package, no subpackages. A `Verifier` interface turns an extracted credential string into a concrete `Identity`; `New` wires an extractor chain (default `BearerHeader()`) in front of one verifier and stores the `Identity` via `core/ctxkey`; `BasicAuth` is a dedicated constant-time constructor. Failures are 401s through a `problem.Responder`.

**Tech Stack:** Go stdlib + forge-internal only: `web/middleware`, `web/problem`, `crypto/consttime`, `core/ctxkey`, `core/errorsx`, `ops/logger`.

## Global Constraints

- Work ONLY in the current branch; never switch branches.
- Module path: `github.com/dmitrymomot/forge`. New package: `auth/guard`.
- Black-box tests only: test files use `package guard_test`.
- After changing files run `just fmt ./auth/guard/` (package-path form — the single-file form trips a spurious betteralign "undefined" error). betteralign may reorder struct fields; accept its output.
- After the final task run `just lint` (includes vet, golangci-lint, nilaway, betteralign, modernize) and `just test ./auth/guard/`.
- In tests use `httptest.NewRequest` — nilaway rejects `http.NewRequest(...)` with a discarded error.
- Use `for i := range n` for counted loops (modernize enforces it); Go 1.26 `new(expr)` allowed, no ptr-wrapper helpers.
- Error sentinels are single-line, `errors.Is`-matchable; client-facing ones are coded via `errorsx.New(code, msg)`.
- Options are `type Option func(*config)` — never builders.
- No Claude/AI attribution lines in any commit message.
- Package size target ~350–450 LOC excluding tests (spec).

---

### Task 1: Foundation — types, errors, context accessors

**Files:**
- Create: `auth/guard/guard.go` (types only in this task; `New` arrives in Task 3)
- Create: `auth/guard/errors.go`
- Create: `auth/guard/context.go`
- Test: `auth/guard/context_test.go`

**Interfaces:**
- Consumes: `core/ctxkey` (`ctxkey.New[T](name)`, `.With/.From/.MustFrom`), `core/errorsx` (`errorsx.New(code, msg string) error`), `ops/logger` (`type ContextExtractor func(ctx context.Context) (slog.Attr, bool)`).
- Produces (used by Tasks 3–5):
  - `type Identity struct { Subject, Tenant string; Scopes []string; Method string; Meta map[string]string }`
  - `type Verifier interface { Verify(ctx context.Context, credential string) (Identity, error) }`
  - `type VerifierFunc func(ctx context.Context, credential string) (Identity, error)` (implements `Verifier`)
  - `var ErrNoCredential, ErrInvalidCredential error` (coded) and `var ErrInvalidUsers error` (plain; consumed by Task 4's ParseUsers)
  - `var identityKey = ctxkey.New[Identity]("guard")` (package-private; Tasks 3–4 store through it)
  - `func From(ctx context.Context) (Identity, bool)`, `func MustFrom(ctx context.Context) Identity`
  - `var LogExtractor logger.ContextExtractor` — emits `slog.Group("auth", slog.String("subject", …)[, slog.String("tenant", …)])`

- [ ] **Step 1: Write the failing test**

`auth/guard/context_test.go`:

```go
package guard_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func TestFrom_Absent(t *testing.T) {
	t.Parallel()
	id, ok := guard.From(context.Background())
	if ok {
		t.Fatalf("From on empty ctx: ok = true, want false (id=%+v)", id)
	}
}

func TestMustFrom_PanicsWhenAbsent(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("MustFrom on empty ctx did not panic")
		}
	}()
	guard.MustFrom(context.Background())
}

func TestVerifierFunc_ImplementsVerifier(t *testing.T) {
	t.Parallel()
	want := guard.Identity{Subject: "u1", Method: "test"}
	var v guard.Verifier = guard.VerifierFunc(func(_ context.Context, cred string) (guard.Identity, error) {
		if cred != "tok" {
			t.Fatalf("credential = %q, want %q", cred, "tok")
		}
		return want, nil
	})
	got, err := v.Verify(context.Background(), "tok")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != want.Subject || got.Method != want.Method {
		t.Fatalf("Verify = %+v, want %+v", got, want)
	}
}

func TestSentinels_AreDistinct(t *testing.T) {
	t.Parallel()
	if errors.Is(guard.ErrNoCredential, guard.ErrInvalidCredential) {
		t.Fatal("ErrNoCredential must not match ErrInvalidCredential")
	}
}

func TestLogExtractor_NoIdentity(t *testing.T) {
	t.Parallel()
	if _, ok := guard.LogExtractor(context.Background()); ok {
		t.Fatal("LogExtractor on empty ctx: ok = true, want false")
	}
}

func TestLogExtractor_SubjectOnly(t *testing.T) {
	t.Parallel()
	// There is no exported setter (only the middleware stores identities), so
	// this test goes through the middleware in Task 3. For now assert only the
	// empty-context contract above; this test body is extended in Task 3.
	t.Skip("extended in Task 3 once New can store an Identity")
	_ = slog.Attr{}
}
```

Note: `From`/`MustFrom`/`LogExtractor` positive paths need the middleware to store an `Identity` (the ctx key is unexported by design). Task 3 replaces the `t.Skip` test with real coverage through `guard.New`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./auth/guard/`
Expected: FAIL — `no required module provides package .../auth/guard` (package does not exist yet).

- [ ] **Step 3: Write the implementation**

`auth/guard/guard.go`:

```go
package guard

import "context"

// Identity is the authenticated principal a Verifier resolved. Subject is
// never empty on a successful verification; Tenant, Scopes, and Meta are
// optional. Scopes is carried for the future authorization decision seam
// (401-vs-403 split) — guard itself never reads it.
type Identity struct {
	Scopes  []string          // permissions/scopes for the future authz seam
	Subject string            // principal id — never empty on success
	Tenant  string            // optional tenant id
	Method  string            // how the request authenticated: "bearer", "session", "apikey", "basic"
	Meta    map[string]string // verifier-specific extras (email, key id, …)
}

// Verifier turns an extracted credential into an Identity. A returned error
// means the credential is rejected: the middleware answers 401 and never
// surfaces error detail to the client. Implementations must be safe for
// concurrent use.
type Verifier interface {
	Verify(ctx context.Context, credential string) (Identity, error)
}

// VerifierFunc adapts a function to Verifier. It doubles as the package's
// test fake — provider adapters (auth/jwt, auth/apikey, auth/session) are
// closures over it or their own types.
type VerifierFunc func(ctx context.Context, credential string) (Identity, error)

// Verify implements Verifier.
func (f VerifierFunc) Verify(ctx context.Context, credential string) (Identity, error) {
	return f(ctx, credential)
}
```

`auth/guard/errors.go`:

```go
package guard

import (
	"errors"

	"github.com/dmitrymomot/forge/core/errorsx"
)

// ErrNoCredential is passed to the responder when no extractor found a
// credential on the request (or the Basic Auth header is missing/malformed).
var ErrNoCredential = errorsx.New("auth_missing", "no credential provided")

// ErrInvalidCredential is passed to the responder when a credential was
// present but rejected — the verifier returned an error (wrapped, so both
// this sentinel and the verifier's own error match via errors.Is), the
// verifier resolved an Identity without a Subject, or Basic Auth
// credentials did not match.
var ErrInvalidCredential = errorsx.New("auth_invalid", "credential rejected")

// ErrInvalidUsers is wrapped in ParseUsers errors for unparseable
// "user:pass,user:pass" credential strings.
var ErrInvalidUsers = errors.New("guard: invalid users string")
```

`auth/guard/context.go`:

```go
package guard

import (
	"context"
	"log/slog"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/logger"
)

var identityKey = ctxkey.New[Identity]("guard")

// From returns the Identity stored by the guard middleware, if any.
func From(ctx context.Context) (Identity, bool) {
	return identityKey.From(ctx)
}

// MustFrom returns the Identity or panics if absent — for handlers mounted
// behind the guard, where a missing Identity is a wiring bug.
func MustFrom(ctx context.Context) Identity {
	return identityKey.MustFrom(ctx)
}

// LogExtractor adds an "auth" group with the subject (and tenant when set)
// for requests that carry an Identity.
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	id, ok := identityKey.From(ctx)
	if !ok {
		return slog.Attr{}, false
	}
	attrs := []any{slog.String("subject", id.Subject)}
	if id.Tenant != "" {
		attrs = append(attrs, slog.String("tenant", id.Tenant))
	}
	return slog.Group("auth", attrs...), true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./auth/guard/`
Expected: PASS (one skip: `TestLogExtractor_SubjectOnly`).

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/guard/
git add auth/guard/
git commit -m "feat(guard): Identity, Verifier seam, coded sentinels, context accessors"
```

---

### Task 2: Extractors

**Files:**
- Create: `auth/guard/extractors.go`
- Test: `auth/guard/extractors_test.go`

**Interfaces:**
- Consumes: nothing from other tasks (stdlib only).
- Produces (used by Tasks 3, 5):
  - `type Extractor func(r *http.Request) (credential string, ok bool)`
  - `func BearerHeader() Extractor`
  - `func Header(name string) Extractor`
  - `func Cookie(name string) Extractor`
  - `func Query(name string) Extractor`

- [ ] **Step 1: Write the failing test**

`auth/guard/extractors_test.go`:

```go
package guard_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func TestBearerHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   string
		wantOK bool
	}{
		{"valid", "Bearer tok123", "tok123", true},
		{"lowercase scheme", "bearer tok123", "tok123", true},
		{"extra spaces after scheme", "Bearer   tok123", "tok123", true},
		{"absent", "", "", false},
		{"scheme only", "Bearer", "", false},
		{"scheme with empty token", "Bearer   ", "", false},
		{"basic scheme reads as no credential", "Basic dXNlcjpwYXNz", "", false},
		{"no scheme", "tok123", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			got, ok := guard.BearerHeader()(r)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("BearerHeader() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-API-Key", "key123")

	if got, ok := guard.Header("X-API-Key")(r); !ok || got != "key123" {
		t.Fatalf("Header(X-API-Key) = (%q, %v), want (key123, true)", got, ok)
	}
	if got, ok := guard.Header("X-Missing")(r); ok {
		t.Fatalf("Header(X-Missing) = (%q, %v), want ok=false", got, ok)
	}
	r.Header.Set("X-Empty", "")
	if _, ok := guard.Header("X-Empty")(r); ok {
		t.Fatal("Header(X-Empty): empty value must read as no credential")
	}
}

func TestCookie(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
	r.AddCookie(&http.Cookie{Name: "empty", Value: ""})

	if got, ok := guard.Cookie("sid")(r); !ok || got != "abc" {
		t.Fatalf("Cookie(sid) = (%q, %v), want (abc, true)", got, ok)
	}
	if _, ok := guard.Cookie("missing")(r); ok {
		t.Fatal("Cookie(missing): want ok=false")
	}
	if _, ok := guard.Cookie("empty")(r); ok {
		t.Fatal("Cookie(empty): empty value must read as no credential")
	}
}

func TestQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/path?token=qtok&empty=", nil)

	if got, ok := guard.Query("token")(r); !ok || got != "qtok" {
		t.Fatalf("Query(token) = (%q, %v), want (qtok, true)", got, ok)
	}
	if _, ok := guard.Query("missing")(r); ok {
		t.Fatal("Query(missing): want ok=false")
	}
	if _, ok := guard.Query("empty")(r); ok {
		t.Fatal("Query(empty): empty value must read as no credential")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./auth/guard/`
Expected: FAIL — `undefined: guard.BearerHeader` (and Header/Cookie/Query).

- [ ] **Step 3: Write the implementation**

`auth/guard/extractors.go`:

```go
package guard

import (
	"net/http"
	"strings"
)

// Extractor pulls a raw credential from a request. ok=false means "not
// present" — the middleware tries the next extractor in the chain; it never
// means "present but bad" (that judgment belongs to the Verifier).
type Extractor func(r *http.Request) (credential string, ok bool)

// BearerHeader extracts the token from an "Authorization: Bearer <token>"
// header (scheme match is case-insensitive). A non-Bearer Authorization
// header reads as no credential, so e.g. a Basic header on a
// bearer-guarded route falls through to the next extractor.
func BearerHeader() Extractor {
	return func(r *http.Request) (string, bool) {
		scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
		if !found || !strings.EqualFold(scheme, "Bearer") {
			return "", false
		}
		token = strings.TrimSpace(token)
		return token, token != ""
	}
}

// Header extracts the named header's value verbatim (e.g. "X-API-Key").
func Header(name string) Extractor {
	return func(r *http.Request) (string, bool) {
		v := r.Header.Get(name)
		return v, v != ""
	}
}

// Cookie extracts the named cookie's value. For signed or encrypted cookies
// write a closure over web/cookie's Codec instead — any func with this
// signature is an Extractor.
func Cookie(name string) Extractor {
	return func(r *http.Request) (string, bool) {
		c, err := r.Cookie(name)
		if err != nil || c.Value == "" {
			return "", false
		}
		return c.Value, true
	}
}

// Query extracts the named query parameter.
//
// Credentials in query strings leak into access logs, browser history, and
// Referer headers. Prefer BearerHeader or Cookie; reserve Query for signed,
// short-lived links.
func Query(name string) Extractor {
	return func(r *http.Request) (string, bool) {
		v := r.URL.Query().Get(name)
		return v, v != ""
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./auth/guard/`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/guard/
git add auth/guard/extractors.go auth/guard/extractors_test.go
git commit -m "feat(guard): credential extractors (bearer, header, cookie, query)"
```

---

### Task 3: Options + New middleware

**Files:**
- Create: `auth/guard/options.go`
- Modify: `auth/guard/guard.go` (append `New`, `extract`, `config.reject`)
- Modify: `auth/guard/context_test.go` (replace the skipped `TestLogExtractor_SubjectOnly`)
- Test: `auth/guard/guard_test.go`, `auth/guard/panic_test.go`

**Interfaces:**
- Consumes: Task 1 (`Identity`, `Verifier`, `VerifierFunc`, `identityKey`, `ErrNoCredential`, `ErrInvalidCredential`), Task 2 (`Extractor`, `BearerHeader`), `web/middleware` (`type Middleware func(http.Handler) http.Handler`), `web/problem` (`type Responder func(w http.ResponseWriter, r *http.Request, err error)`, `problem.JSON(problem.WithStatus(401))`).
- Produces (used by Tasks 4, 5):
  - `func New(v Verifier, opts ...Option) middleware.Middleware`
  - `type Option func(*config)` with `WithExtractors(...Extractor)`, `WithOptional()`, `WithResponder(problem.Responder)`, `WithChallenge(string)`, `WithRealm(string)` (realm validated here; consumed by Task 4)
  - unexported: `config` struct (fields `extractors []Extractor`, `challenge, realm string`, `responder problem.Responder`, `optional bool`), `func (c config) reject(w, r, err)` setting `WWW-Authenticate` from `c.challenge` before invoking the responder

- [ ] **Step 1: Write the failing tests**

`auth/guard/guard_test.go`:

```go
package guard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

// okVerifier accepts credential "good" as user u1; everything else fails
// with errBadToken.
var errBadToken = errors.New("bad token")

func okVerifier() guard.Verifier {
	return guard.VerifierFunc(func(_ context.Context, cred string) (guard.Identity, error) {
		if cred == "good" {
			return guard.Identity{Subject: "u1", Tenant: "t1", Method: "bearer"}, nil
		}
		return guard.Identity{}, errBadToken
	})
}

// echoHandler writes the context Identity's subject, proving the request
// passed the guard and the Identity is readable.
func echoHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := guard.From(r.Context())
		if !ok {
			_, _ = w.Write([]byte("anonymous"))
			return
		}
		_, _ = w.Write([]byte(id.Subject))
	})
}

func get(t *testing.T, h http.Handler, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestNew_ValidCredential(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier())(echoHandler(t))
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Code != http.StatusOK || w.Body.String() != "u1" {
		t.Fatalf("got %d %q, want 200 u1", w.Code, w.Body.String())
	}
}

func TestNew_MissingCredential(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier())(echoHandler(t))
	w := get(t, h, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("WWW-Authenticate set without WithChallenge")
	}
}

func TestNew_InvalidCredential(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier())(echoHandler(t))
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") })
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "bad token") {
		t.Fatalf("verifier error leaked to client: %q", body)
	}
}

func TestNew_ErrorWrapping(t *testing.T) {
	t.Parallel()
	var captured error
	responder := func(w http.ResponseWriter, r *http.Request, err error) {
		captured = err
		w.WriteHeader(http.StatusUnauthorized)
	}

	h := guard.New(okVerifier(), guard.WithResponder(responder))(echoHandler(t))
	get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") })
	if !errors.Is(captured, guard.ErrInvalidCredential) {
		t.Fatalf("err = %v, want Is(ErrInvalidCredential)", captured)
	}
	if !errors.Is(captured, errBadToken) {
		t.Fatalf("err = %v, want Is(errBadToken) — verifier error must stay matchable", captured)
	}

	get(t, h, nil)
	if !errors.Is(captured, guard.ErrNoCredential) {
		t.Fatalf("err = %v, want Is(ErrNoCredential)", captured)
	}
}

func TestNew_EmptySubjectIsRejected(t *testing.T) {
	t.Parallel()
	var captured error
	v := guard.VerifierFunc(func(context.Context, string) (guard.Identity, error) {
		return guard.Identity{}, nil // buggy verifier: success with zero Identity
	})
	h := guard.New(v, guard.WithResponder(func(w http.ResponseWriter, r *http.Request, err error) {
		captured = err
		w.WriteHeader(http.StatusUnauthorized)
	}))(echoHandler(t))
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !errors.Is(captured, guard.ErrInvalidCredential) {
		t.Fatalf("err = %v, want Is(ErrInvalidCredential)", captured)
	}
}

func TestNew_Optional(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier(), guard.WithOptional())(echoHandler(t))

	// Missing credential: anonymous pass-through.
	w := get(t, h, nil)
	if w.Code != http.StatusOK || w.Body.String() != "anonymous" {
		t.Fatalf("optional missing: got %d %q, want 200 anonymous", w.Code, w.Body.String())
	}
	// Valid credential: identity attached.
	w = get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Code != http.StatusOK || w.Body.String() != "u1" {
		t.Fatalf("optional valid: got %d %q, want 200 u1", w.Code, w.Body.String())
	}
	// Invalid credential: still 401.
	w = get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") })
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("optional invalid: status = %d, want 401", w.Code)
	}
}

func TestNew_Challenge(t *testing.T) {
	t.Parallel()
	h := guard.New(okVerifier(), guard.WithChallenge(`Bearer realm="api"`))(echoHandler(t))

	for name, mutate := range map[string]func(*http.Request){
		"missing": nil,
		"invalid": func(r *http.Request) { r.Header.Set("Authorization", "Bearer evil") },
	} {
		w := get(t, h, mutate)
		if got := w.Header().Get("WWW-Authenticate"); got != `Bearer realm="api"` {
			t.Fatalf("%s: WWW-Authenticate = %q, want Bearer realm=\"api\"", name, got)
		}
	}

	// Success must not carry the challenge.
	w := get(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") })
	if w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("WWW-Authenticate set on success")
	}
}

func TestNew_ExtractorChainOrder(t *testing.T) {
	t.Parallel()
	var seen []string
	v := guard.VerifierFunc(func(_ context.Context, cred string) (guard.Identity, error) {
		seen = append(seen, cred)
		return guard.Identity{Subject: cred, Method: "test"}, nil
	})
	h := guard.New(v, guard.WithExtractors(
		guard.Header("X-First"),
		guard.Header("X-Second"),
	))(echoHandler(t))

	// Both present: first extractor wins, second never consulted.
	w := get(t, h, func(r *http.Request) {
		r.Header.Set("X-First", "one")
		r.Header.Set("X-Second", "two")
	})
	if w.Body.String() != "one" {
		t.Fatalf("body = %q, want one (first extractor wins)", w.Body.String())
	}
	// First absent: chain falls through to the second.
	w = get(t, h, func(r *http.Request) { r.Header.Set("X-Second", "two") })
	if w.Body.String() != "two" {
		t.Fatalf("body = %q, want two (fallthrough)", w.Body.String())
	}
	if len(seen) != 2 {
		t.Fatalf("verifier calls = %d, want 2 (one per request)", len(seen))
	}
}

func TestNew_NoVerifyFallbackAfterExtract(t *testing.T) {
	t.Parallel()
	calls := 0
	v := guard.VerifierFunc(func(context.Context, string) (guard.Identity, error) {
		calls++
		return guard.Identity{}, errBadToken
	})
	h := guard.New(v, guard.WithExtractors(
		guard.Header("X-First"),
		guard.Header("X-Second"),
	))(echoHandler(t))

	// First extractor hits, verify fails: 401 — the second extractor must
	// NOT be offered as a fallback.
	w := get(t, h, func(r *http.Request) {
		r.Header.Set("X-First", "bad")
		r.Header.Set("X-Second", "good")
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if calls != 1 {
		t.Fatalf("verifier calls = %d, want 1 (no fallback after extraction)", calls)
	}
}
```

`auth/guard/panic_test.go`:

```go
package guard_test

import (
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", name)
		}
	}()
	fn()
}

func TestNew_NilVerifierPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "New(nil)", func() { guard.New(nil) })
}

func TestWithExtractors_EmptyPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "WithExtractors()", func() {
		guard.New(okVerifier(), guard.WithExtractors())
	})
}
```

Replace the skipped test in `auth/guard/context_test.go` (delete `TestLogExtractor_SubjectOnly` with its `t.Skip` and the now-unused `log/slog` import if nothing else uses it) with:

```go
func TestLogExtractor_ThroughMiddleware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		id     guard.Identity
		tenant bool
	}{
		{"subject only", guard.Identity{Subject: "u1", Method: "test"}, false},
		{"subject and tenant", guard.Identity{Subject: "u1", Tenant: "t1", Method: "test"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := guard.VerifierFunc(func(context.Context, string) (guard.Identity, error) {
				return tt.id, nil
			})
			var attr slog.Attr
			var ok bool
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attr, ok = guard.LogExtractor(r.Context())
			})
			h := guard.New(v)(inner)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Authorization", "Bearer tok")
			h.ServeHTTP(httptest.NewRecorder(), r)

			if !ok {
				t.Fatal("LogExtractor: ok = false behind the guard")
			}
			if attr.Key != "auth" {
				t.Fatalf("attr key = %q, want auth", attr.Key)
			}
			s := attr.Value.String()
			if !strings.Contains(s, "u1") {
				t.Fatalf("attr %q does not contain subject", s)
			}
			if tt.tenant != strings.Contains(s, "t1") {
				t.Fatalf("attr %q tenant presence, want %v", s, tt.tenant)
			}
		})
	}
}
```

(Add `net/http`, `net/http/httptest`, `strings` to `context_test.go` imports.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/guard/`
Expected: FAIL — `undefined: guard.New`, `undefined: guard.WithResponder`, etc.

- [ ] **Step 3: Write the implementation**

`auth/guard/options.go`:

```go
package guard

import (
	"strings"

	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	extractors []Extractor
	challenge  string
	realm      string
	responder  problem.Responder
	optional   bool
}

// Option configures New and BasicAuth. Options that don't apply to a
// constructor are ignored by it (each option documents which constructor
// reads it).
type Option func(*config)

// WithExtractors replaces the default extractor chain (BearerHeader only).
// Extractors run in order and the first hit wins — later extractors are not
// fallbacks for a credential that fails verification. Panics on an empty
// list: a guard that can never find a credential is a wiring bug. BasicAuth
// ignores this option (its scheme is fixed).
func WithExtractors(xs ...Extractor) Option {
	if len(xs) == 0 {
		panic("guard: WithExtractors requires at least one extractor")
	}
	return func(c *config) { c.extractors = xs }
}

// WithOptional lets requests without a credential pass through anonymously
// (From reports ok=false). A present-but-invalid credential still gets 401 —
// silently ignoring bad tokens masks expired sessions and probing. BasicAuth
// ignores this option.
func WithOptional() Option {
	return func(c *config) { c.optional = true }
}

// WithResponder overrides the rejection response (default problem.JSON 401).
// The error passed to the responder matches guard.ErrNoCredential or
// guard.ErrInvalidCredential via errors.Is; verify failures also match the
// verifier's own error.
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

// WithChallenge sets the WWW-Authenticate header value emitted with every
// 401, e.g. `Bearer realm="api"` (RFC 6750). Default: none. BasicAuth
// ignores this option — it always emits its own Basic challenge.
func WithChallenge(v string) Option {
	return func(c *config) { c.challenge = v }
}

// WithRealm sets the Basic Auth realm (default "restricted"). Panics on
// quotes, backslashes, or control characters — the realm is echoed into the
// WWW-Authenticate header. New ignores this option.
func WithRealm(realm string) Option {
	if strings.ContainsAny(realm, "\"\\") || strings.ContainsFunc(realm, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		panic("guard: realm must not contain quotes, backslashes, or control characters")
	}
	return func(c *config) { c.realm = realm }
}
```

Append to `auth/guard/guard.go`:

```go
// New returns middleware that authenticates every request through v: the
// extractor chain (default BearerHeader) finds the credential, v resolves
// it to an Identity stored in request context for From/MustFrom. A request
// with no credential gets 401 (or passes anonymously with WithOptional); a
// rejected credential gets 401 always. Rejections go through the responder
// (default problem.JSON 401) after setting WWW-Authenticate when a
// challenge is configured. New panics on a nil verifier — a wiring bug.
func New(v Verifier, opts ...Option) middleware.Middleware {
	if v == nil {
		panic("guard: nil verifier")
	}
	cfg := config{
		responder:  problem.JSON(problem.WithStatus(http.StatusUnauthorized)),
		extractors: []Extractor{BearerHeader()},
	}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cred, found := extract(cfg.extractors, r)
			if !found {
				if cfg.optional {
					next.ServeHTTP(w, r)
					return
				}
				cfg.reject(w, r, ErrNoCredential)
				return
			}
			id, err := v.Verify(r.Context(), cred)
			if err != nil {
				cfg.reject(w, r, fmt.Errorf("%w: %w", ErrInvalidCredential, err))
				return
			}
			if id.Subject == "" {
				// A successful verification with no Subject is a verifier
				// bug; treat it as a rejection rather than admitting an
				// anonymous principal.
				cfg.reject(w, r, ErrInvalidCredential)
				return
			}
			next.ServeHTTP(w, r.WithContext(identityKey.With(r.Context(), id)))
		})
	}
}

func extract(xs []Extractor, r *http.Request) (string, bool) {
	for _, x := range xs {
		if cred, ok := x(r); ok {
			return cred, true
		}
	}
	return "", false
}

func (c config) reject(w http.ResponseWriter, r *http.Request, err error) {
	if c.challenge != "" {
		w.Header().Set("WWW-Authenticate", c.challenge)
	}
	c.responder(w, r, err)
}
```

Update `guard.go` imports to:

```go
import (
	"context"
	"fmt"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/guard/`
Expected: PASS, no skips, race-clean.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/guard/
git add auth/guard/
git commit -m "feat(guard): New middleware — extractor chain, optional mode, challenge, problem 401s"
```

---

### Task 4: BasicAuth + ParseUsers

**Files:**
- Create: `auth/guard/basicauth.go`
- Test: `auth/guard/basicauth_test.go`
- Modify: `auth/guard/panic_test.go` (add BasicAuth panic case)

**Interfaces:**
- Consumes: Task 1 (`Identity`, `identityKey`, `ErrNoCredential`, `ErrInvalidCredential`, `ErrInvalidUsers`), Task 3 (`config`, `Option`, `WithRealm`, `WithResponder`; test helpers `echoHandler(t)` from `guard_test.go` and `mustPanic(t, name, fn)` from `panic_test.go` — same `guard_test` package, already defined), `crypto/consttime` (`consttime.StringEqual(a, b string) bool`).
- Produces (used by Task 5):
  - `func BasicAuth(users map[string]string, opts ...Option) middleware.Middleware`
  - `func ParseUsers(s string) (map[string]string, error)`

- [ ] **Step 1: Write the failing tests**

`auth/guard/basicauth_test.go`:

```go
package guard_test

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func basicGet(t *testing.T, h http.Handler, setAuth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if setAuth != nil {
		setAuth(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestBasicAuth_Success(t *testing.T) {
	t.Parallel()
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"})(echoHandler(t))
	w := basicGet(t, h, func(r *http.Request) { r.SetBasicAuth("ops", "s3cret") })
	if w.Code != http.StatusOK || w.Body.String() != "ops" {
		t.Fatalf("got %d %q, want 200 ops (Identity in context)", w.Code, w.Body.String())
	}
	if w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("WWW-Authenticate set on success")
	}
}

func TestBasicAuth_MethodIsBasic(t *testing.T) {
	t.Parallel()
	var got guard.Identity
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = guard.MustFrom(r.Context())
	})
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"})(inner)
	basicGet(t, h, func(r *http.Request) { r.SetBasicAuth("ops", "s3cret") })
	if got.Subject != "ops" || got.Method != "basic" {
		t.Fatalf("Identity = %+v, want Subject=ops Method=basic", got)
	}
}

func TestBasicAuth_Failures(t *testing.T) {
	t.Parallel()
	const wantChallenge = `Basic realm="restricted", charset="UTF-8"`
	tests := []struct {
		name    string
		setAuth func(*http.Request)
		wantErr error
	}{
		{"missing header", nil, guard.ErrNoCredential},
		{"malformed header", func(r *http.Request) { r.Header.Set("Authorization", "Basic !!!not-base64!!!") }, guard.ErrNoCredential},
		{"wrong password", func(r *http.Request) { r.SetBasicAuth("ops", "wrong") }, guard.ErrInvalidCredential},
		{"unknown user", func(r *http.Request) { r.SetBasicAuth("nobody", "s3cret") }, guard.ErrInvalidCredential},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured error
			h := guard.BasicAuth(map[string]string{"ops": "s3cret"},
				guard.WithResponder(func(w http.ResponseWriter, r *http.Request, err error) {
					captured = err
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)(echoHandler(t))
			w := basicGet(t, h, tt.setAuth)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if got := w.Header().Get("WWW-Authenticate"); got != wantChallenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, wantChallenge)
			}
			if !errors.Is(captured, tt.wantErr) {
				t.Fatalf("err = %v, want Is(%v)", captured, tt.wantErr)
			}
		})
	}
}

func TestBasicAuth_Realm(t *testing.T) {
	t.Parallel()
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"}, guard.WithRealm("staging"))(echoHandler(t))
	w := basicGet(t, h, nil)
	if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="staging", charset="UTF-8"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
}

func TestBasicAuth_DefaultProblemResponse(t *testing.T) {
	t.Parallel()
	h := guard.BasicAuth(map[string]string{"ops": "s3cret"})(echoHandler(t))
	w := basicGet(t, h, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
}

func TestParseUsers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    string
		want  map[string]string
		isErr bool
	}{
		{"single", "ops:s3cret", map[string]string{"ops": "s3cret"}, false},
		{"multiple with space", "a:1, b:2", map[string]string{"a": "1", "b": "2"}, false},
		{"password with colon", "ops:pa:ss", map[string]string{"ops": "pa:ss"}, false},
		{"empty input", "", nil, true},
		{"no colon", "opspass", nil, true},
		{"empty user", ":pass", nil, true},
		{"empty password", "ops:", nil, true},
		{"duplicate user", "ops:1,ops:2", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := guard.ParseUsers(tt.in)
			if tt.isErr {
				if !errors.Is(err, guard.ErrInvalidUsers) {
					t.Fatalf("err = %v, want Is(ErrInvalidUsers)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseUsers(%q): %v", tt.in, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Fatalf("ParseUsers(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
```

Append to `auth/guard/panic_test.go`:

```go
func TestBasicAuth_EmptyUsersPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "BasicAuth(nil)", func() { guard.BasicAuth(nil) })
	mustPanic(t, "BasicAuth(empty)", func() { guard.BasicAuth(map[string]string{}) })
}

func TestWithRealm_InvalidPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, `WithRealm(quote)`, func() { guard.WithRealm(`sta"ging`) })
	mustPanic(t, "WithRealm(control)", func() { guard.WithRealm("sta\nging") })
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./auth/guard/`
Expected: FAIL — `undefined: guard.BasicAuth`, `undefined: guard.ParseUsers`.

- [ ] **Step 3: Write the implementation**

`auth/guard/basicauth.go`:

```go
package guard

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// dummyPassword is compared for unknown usernames so user existence does
// not leak through response timing.
const dummyPassword = "guard-basicauth-dummy-password-for-unknown-users"

// BasicAuth returns middleware gating requests with HTTP Basic Auth against
// a static username→password map — for pprof/metrics/staging/admin gates,
// never user login (no hashing; credentials come from env, see ParseUsers).
// Password checks are constant-time and unknown users cost the same as
// wrong passwords; unknown-user and wrong-password failures are
// indistinguishable to the client. Every failure gets 401 with a
// WWW-Authenticate Basic challenge through the responder (default
// problem.JSON 401). On success the request carries
// Identity{Subject: username, Method: "basic"} — From/MustFrom work as
// behind New. Panics on an empty users map — a gate with no valid
// credentials is a wiring bug. Accepted options: WithRealm, WithResponder;
// WithExtractors, WithOptional, and WithChallenge are ignored (the scheme
// and challenge are fixed).
func BasicAuth(users map[string]string, opts ...Option) middleware.Middleware {
	if len(users) == 0 {
		panic("guard: BasicAuth requires at least one user")
	}
	cfg := config{
		responder: problem.JSON(problem.WithStatus(http.StatusUnauthorized)),
		realm:     "restricted",
	}
	for _, o := range opts {
		o(&cfg)
	}
	challenge := fmt.Sprintf("Basic realm=%q, charset=%q", cfg.realm, "UTF-8")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reject := func(err error) {
				w.Header().Set("WWW-Authenticate", challenge)
				cfg.responder(w, r, err)
			}
			user, pass, ok := r.BasicAuth()
			if !ok {
				reject(ErrNoCredential)
				return
			}
			want, known := users[user]
			if !known {
				want = dummyPassword
			}
			if !consttime.StringEqual(pass, want) || !known {
				reject(ErrInvalidCredential)
				return
			}
			next.ServeHTTP(w, r.WithContext(identityKey.With(r.Context(), Identity{Subject: user, Method: "basic"})))
		})
	}
}

// ParseUsers parses BasicAuth credentials from the "user1:pass1,user2:pass2"
// env-string format. Passwords may contain colons (the split is at the first
// colon) but not commas. It rejects (wrapping ErrInvalidUsers) empty input,
// entries without a colon, empty usernames or passwords, and duplicate
// usernames.
func ParseUsers(s string) (map[string]string, error) {
	entries := strings.Split(s, ",")
	users := make(map[string]string, len(entries))
	for _, e := range entries {
		user, pass, found := strings.Cut(strings.TrimSpace(e), ":")
		if !found || user == "" || pass == "" {
			return nil, fmt.Errorf("%w: entry %q", ErrInvalidUsers, e)
		}
		if _, dup := users[user]; dup {
			return nil, fmt.Errorf("%w: duplicate user %q", ErrInvalidUsers, user)
		}
		users[user] = pass
	}
	return users, nil
}
```

Note on the comparison: `consttime.StringEqual` runs first and `|| !known` second, so the constant-time compare always executes; the `known` flag only flips the outcome for the (already-compared) dummy path — including when a client happens to present the dummy password itself.

- [ ] **Step 4: Run tests to verify they pass**

Run: `just test ./auth/guard/`
Expected: PASS, race-clean.

- [ ] **Step 5: Format and commit**

```bash
just fmt ./auth/guard/
git add auth/guard/
git commit -m "feat(guard): constant-time BasicAuth gate and ParseUsers env-format helper"
```

---

### Task 5: doc.go, catalog updates, full verification

**Files:**
- Create: `auth/guard/doc.go`
- Modify: `docs/packages.md` (delete the `auth/guard` entry; un-tag three dependents)

**Interfaces:**
- Consumes: the complete public API from Tasks 1–4 (doc.go references `New`, `VerifierFunc`, `Identity`, `WithChallenge`, `WithExtractors`, `Cookie`, `From`, `BasicAuth`, `ParseUsers`, `WithRealm`).
- Produces: nothing new — documentation and roadmap bookkeeping.

- [ ] **Step 1: Write doc.go**

`auth/guard/doc.go`:

```go
// Package guard authenticates HTTP requests. A chain of credential
// extractors finds the credential (Authorization header by default;
// cookie and query opt-in), a Verifier resolves it to an Identity stored
// in request context, and rejections are problem+json 401s. Authorization
// (403, permissions) is out of scope — guard answers "who is this request
// from"; session lifecycle (rotation, TTLs) belongs to auth/session and
// scope checks to the future authorization seam.
//
// A Verifier adapter is a small closure — over auth/jwt:
//
//	type appClaims struct {
//		jwt.Claims
//		TenantID string   `json:"tid"`
//		Scopes   []string `json:"scopes"`
//	}
//
//	verifier := guard.VerifierFunc(func(ctx context.Context, token string) (guard.Identity, error) {
//		c, err := jwt.Verify[appClaims](ctx, jwtVerifier, token)
//		if err != nil {
//			return guard.Identity{}, err // client sees a generic 401
//		}
//		return guard.Identity{Subject: c.Subject, Tenant: c.TenantID, Scopes: c.Scopes, Method: "bearer"}, nil
//	})
//
//	authn := guard.New(verifier, guard.WithChallenge(`Bearer realm="api"`))
//	mux.Handle("GET /api/me", authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		id := guard.MustFrom(r.Context())
//		_ = id.Subject
//	})))
//
// Session-cookie flows swap the extractor and redirect instead of 401:
//
//	authn := guard.New(sessionVerifier,
//		guard.WithExtractors(guard.Cookie("sid")),
//		guard.WithResponder(func(w http.ResponseWriter, r *http.Request, err error) {
//			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
//		}),
//	)
//
// Public-but-personalized routes use WithOptional: missing credential
// passes anonymously (From reports ok=false), invalid credential still
// gets 401.
//
// BasicAuth gates internal surfaces (pprof, metrics, staging) with static
// env-sourced credentials, constant-time:
//
//	users, err := guard.ParseUsers(os.Getenv("ADMIN_BASIC_USERS")) // "ops:s3cret"
//	if err != nil {
//		log.Fatal(err)
//	}
//	mux.Handle("/debug/pprof/", guard.BasicAuth(users, guard.WithRealm("staging"))(pprofHandler))
package guard
```

- [ ] **Step 2: Update docs/packages.md**

Delete the whole `auth/guard` entry (the `**auth/guard**` heading, its paragraph, its `Deps:` line, and one surrounding `---` separator — currently lines 621–631).

Un-tag the three dependents (guard is now shipped):

- `auth/apikey` entry: `Deps: `core/random`, `crypto/consttime`; `auth/guard` (planned).` → `Deps: `core/random`, `crypto/consttime`, `auth/guard`.`
- `auth/scim` entry: `Deps: `auth/guard` (planned).` → `Deps: `auth/guard`.`
- `ops/debug` entry: `Deps: `ops/supervisor`; `auth/guard` (planned).` → `Deps: `ops/supervisor`, `auth/guard`.`

Verify no stragglers: `grep -n "auth/guard" docs/packages.md` must show only the three un-tagged dependent lines.

- [ ] **Step 3: Full verification**

```bash
just fmt ./auth/guard/
just lint
just test ./auth/guard/
```

Expected: lint clean (vet, build, golangci-lint, nilaway, betteralign, modernize), tests PASS with race detector and coverage reported. If a stale "undefined" LSP diagnostic appears after subagent work, trust the build/test output at HEAD, not the diagnostic.

Sanity-check package size (target ~350–450 LOC excluding tests):

```bash
wc -l auth/guard/*.go | grep -v _test
```

- [ ] **Step 4: Commit**

```bash
git add auth/guard/doc.go docs/packages.md
git commit -m "docs(guard): package docs; mark auth/guard shipped in catalog"
```

---

## Verification Checklist (post-plan)

- [ ] `just test ./auth/guard/` — all green, race-clean
- [ ] `just lint` — clean
- [ ] `grep -rn "IdentityFromContext" auth/guard/` — empty (reader is `From`/`MustFrom`)
- [ ] `grep -n "auth/guard" docs/packages.md` — only the three dependent `Deps:` lines
- [ ] doc.go compiles as prose examples (no Example funcs needed; ipfilter precedent)
- [ ] No commit message contains AI attribution
