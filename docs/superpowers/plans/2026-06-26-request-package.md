# request Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone `request` package of stateless, reflection-free free functions that read data off an `*http.Request` into Go values — typed accessors, strict body decoding, and focused readers (BearerToken, ClientIP, pagination).

**Architecture:** Every typed accessor funnels a raw string through one private generic engine (`parse[T]`: scalar type-switch → `time.Duration` → `encoding.TextUnmarshaler`) and a shared `resolve` helper that applies the missing/default/malformed contract. Per-source wrappers (`Query`, `Path`, `Header`, `Cookie`, `FormValue`) differ only in how they pull the raw string. Body decoding, ClientIP, and pagination each carry their own concern-scoped functional-option type. Failures are one typed `*Error{Source, Key, Kind, Err}`; `StatusCode` maps it to 400/413/415.

**Tech Stack:** Go 1.26 standard library (`encoding`, `encoding/json`, `errors`, `fmt`, `mime`, `mime/multipart`, `net`, `net/http`, `net/netip`, `strconv`, `strings`, `time`); `testify` in tests only; `just` task runner.

**Spec:** [docs/superpowers/specs/2026-06-26-request-package-design.md](../specs/2026-06-26-request-package-design.md)

## Global Constraints

- **Go 1.26**, module `github.com/dmitrymomot/forge`; new package import path `github.com/dmitrymomot/forge/request`.
- **Stdlib only** in package code — no external dependencies. `testify` (already a module dep) allowed in tests only.
- **Black-box tests ONLY** — every `*_test.go` declares `package request_test` and imports the package.
- **Flat layout** — files live directly under `request/`, no subfolders.
- **No reflection, no struct tags, no `init()`, no global mutable state** — interface assertions only (`encoding.TextUnmarshaler`).
- **No builder pattern** — options are functional, split into three concern-scoped types: `BodyOption`, `ClientIPOption`, `PageOption`.
- **Single-line errors**, prefixed `request:` and `%w`-wrapped; the wrapped cause stays reachable via `Unwrap`. No embedded stacks/blobs.
- **Use `just` recipes** — `just test ./request/` to run; `just check` (fmt + lint + test) before finishing.
- The accessor contract: absent/empty value ⇒ `def[0]` or the zero value with nil error; present-but-unparseable ⇒ `(zero, *Error{Kind: Malformed})`.

---

### Task 1: Error model and package doc

**Files:**
- Create: `request/doc.go`
- Create: `request/errors.go`
- Test: `request/errors_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Source string` with `SourceQuery`, `SourcePath`, `SourceHeader`, `SourceCookie`, `SourceForm`, `SourceBody`.
  - `type Kind int` with `KindMalformed`, `KindTooLarge`, `KindUnsupportedMediaType`, `KindInvalidBody`; `func (Kind) String() string`.
  - `type Error struct { Source Source; Key string; Kind Kind; Err error }`; `func (*Error) Error() string`; `func (*Error) Unwrap() error`.
  - `func StatusCode(err error) int` — used by every later task's tests and consumers.

- [ ] **Step 1: Write the failing test**

Create `request/errors_test.go`:

```go
package request_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
)

func TestStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil is 200", nil, http.StatusOK},
		{"malformed is 400", &request.Error{Source: request.SourceQuery, Key: "p", Kind: request.KindMalformed}, http.StatusBadRequest},
		{"too large is 413", &request.Error{Source: request.SourceBody, Kind: request.KindTooLarge}, http.StatusRequestEntityTooLarge},
		{"unsupported media is 415", &request.Error{Source: request.SourceBody, Kind: request.KindUnsupportedMediaType}, http.StatusUnsupportedMediaType},
		{"invalid body is 400", &request.Error{Source: request.SourceBody, Kind: request.KindInvalidBody}, http.StatusBadRequest},
		{"plain error is 400", errors.New("other"), http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, request.StatusCode(tc.err))
		})
	}
}

func TestErrorStringAndUnwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")
	e := &request.Error{Source: request.SourceQuery, Key: "page", Kind: request.KindMalformed, Err: cause}

	assert.ErrorIs(t, e, cause)
	assert.Contains(t, e.Error(), "request:")
	assert.Contains(t, e.Error(), "query")
	assert.Contains(t, e.Error(), "page")
	assert.Contains(t, e.Error(), "malformed")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`no non-test Go files`, or `undefined: request.Error`).

- [ ] **Step 3: Write the package doc and the error model**

Create `request/doc.go`:

```go
// Package request provides small, stateless, reflection-free helpers for reading
// data off an *http.Request into Go values: typed accessors for query, path,
// header, cookie, and form values (Query, Path, Header, Cookie, FormValue and
// their Func/Slice/Split variants), strict body decoding (DecodeJSON, RawBody,
// multipart File/Files), and focused readers (BearerToken, ClientIP, presence
// predicates, QueryPage/QueryCursor).
//
// The helpers are free functions — no constructor, no binder, no struct tags, no
// global state. Each typed accessor returns the zero value of T and a nil error
// when the key is absent, and a *request.Error only when a present value fails to
// parse. request.StatusCode maps that error to the right HTTP status (400/413/415):
//
//	page, err := request.Query[int](r, "page", 1)
//	if err != nil {
//		render.JSON(w, request.StatusCode(err), apiErr{err.Error()})
//		return
//	}
//
// Custom types parse with no reflection: any type whose pointer implements
// encoding.TextUnmarshaler (uuid.UUID, netip.Addr, time.Time, custom enums) works
// through the generic engine; anything else can use the Func variants.
//
// request reads; the render package writes; the htmx package handles HX-* headers.
// None imports another. Stdlib only.
package request
```

Create `request/errors.go`:

```go
package request

import (
	"errors"
	"fmt"
	"net/http"
)

// Source identifies which part of the request a value came from.
type Source string

const (
	SourceQuery  Source = "query"
	SourcePath   Source = "path"
	SourceHeader Source = "header"
	SourceCookie Source = "cookie"
	SourceForm   Source = "form"
	SourceBody   Source = "body"
)

// Kind classifies a request-reading failure so handlers can map it to a status.
type Kind int

const (
	KindMalformed            Kind = iota // unparseable value           -> 400
	KindTooLarge                         // body exceeded the size cap   -> 413
	KindUnsupportedMediaType             // wrong/absent Content-Type    -> 415
	KindInvalidBody                      // malformed/unknown-field JSON -> 400
)

// String returns a short human label for k.
func (k Kind) String() string {
	switch k {
	case KindMalformed:
		return "malformed"
	case KindTooLarge:
		return "too large"
	case KindUnsupportedMediaType:
		return "unsupported media type"
	case KindInvalidBody:
		return "invalid body"
	default:
		return "unknown"
	}
}

// Error is the single error type returned by every request-reading helper. Source
// and Key name the offending input; Kind drives StatusCode; Err is the cause.
type Error struct {
	Source Source
	Key    string
	Kind   Kind
	Err    error
}

// Error returns a single-line description; the wrapped cause is reached via Unwrap.
func (e *Error) Error() string {
	loc := string(e.Source)
	if e.Key != "" {
		loc = fmt.Sprintf("%s %q", e.Source, e.Key)
	}
	if e.Err != nil {
		return fmt.Sprintf("request: %s: %s: %v", loc, e.Kind, e.Err)
	}
	return fmt.Sprintf("request: %s: %s", loc, e.Kind)
}

// Unwrap returns the wrapped cause so errors.Is/As reach it.
func (e *Error) Unwrap() error { return e.Err }

// StatusCode reports the HTTP status for err: a *Error maps by Kind
// (400/413/415); any other non-nil error is 400; nil is 200.
func StatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if e, ok := errors.AsType[*Error](err); ok {
		switch e.Kind {
		case KindTooLarge:
			return http.StatusRequestEntityTooLarge
		case KindUnsupportedMediaType:
			return http.StatusUnsupportedMediaType
		default:
			return http.StatusBadRequest
		}
	}
	return http.StatusBadRequest
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS — `ok  github.com/dmitrymomot/forge/request`.

- [ ] **Step 5: Commit**

```bash
git add request/doc.go request/errors.go request/errors_test.go
git commit -m "feat(request): typed error model and StatusCode mapper"
```

---

### Task 2: Generic parse engine and the Query accessor family

**Files:**
- Create: `request/parse.go`
- Create: `request/query.go`
- Test: `request/query_test.go`

**Interfaces:**
- Consumes: `Source`, `Error`, `KindMalformed` (Task 1).
- Produces:
  - Internal `func parse[T any](raw string) (T, error)`; internal `resolve`, `resolveSlice`, `resolveSplit` — reused by Tasks 3 and 4.
  - `func Query[T any](r *http.Request, key string, def ...T) (T, error)`
  - `func QueryFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error)`
  - `func QuerySlice[T any](r *http.Request, key string, def ...[]T) ([]T, error)`
  - `func QuerySliceFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...[]T) ([]T, error)`
  - `func QuerySplit[T any](r *http.Request, key, sep string, def ...[]T) ([]T, error)`
  - `func QuerySplitFunc[T any](r *http.Request, key, sep string, parse func(string) (T, error), def ...[]T) ([]T, error)`
  - `func HasQuery(r *http.Request, key string) bool`

- [ ] **Step 1: Write the failing test**

Create `request/query_test.go`:

```go
package request_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// color is a local TextUnmarshaler type — proves custom types parse with no
// external dependency and no reflection.
type color struct{ name string }

func (c *color) UnmarshalText(b []byte) error {
	switch s := string(b); s {
	case "red", "green", "blue":
		c.name = s
		return nil
	default:
		return fmt.Errorf("bad color %q", s)
	}
}

func TestQueryScalars(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?n=42&ok=true&f=3.5&s=hi&d=1500ms", nil)

	n, err := request.Query[int](r, "n")
	require.NoError(t, err)
	assert.Equal(t, 42, n)

	ok, err := request.Query[bool](r, "ok")
	require.NoError(t, err)
	assert.True(t, ok)

	f, err := request.Query[float64](r, "f")
	require.NoError(t, err)
	assert.InEpsilon(t, 3.5, f, 1e-9)

	s, err := request.Query[string](r, "s")
	require.NoError(t, err)
	assert.Equal(t, "hi", s)

	d, err := request.Query[time.Duration](r, "d")
	require.NoError(t, err)
	assert.Equal(t, 1500*time.Millisecond, d)
}

func TestQueryTextUnmarshaler(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?c=blue", nil)
	c, err := request.Query[color](r, "c")
	require.NoError(t, err)
	assert.Equal(t, "blue", c.name)
}

func TestQueryMalformed(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?n=oops", nil)

	_, err := request.Query[int](r, "n")
	require.Error(t, err)

	var re *request.Error
	require.ErrorAs(t, err, &re)
	require.NotNil(t, re)
	if re != nil { // nil-guard so nilaway can follow the deref
		assert.Equal(t, request.SourceQuery, re.Source)
		assert.Equal(t, request.KindMalformed, re.Kind)
		assert.Equal(t, "n", re.Key)
	}
}

func TestQueryAbsentAndDefault(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	n, err := request.Query[int](r, "n")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	n2, err := request.Query[int](r, "n", 7)
	require.NoError(t, err)
	assert.Equal(t, 7, n2)
}

func TestQueryFunc(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?hex=ff", nil)
	v, err := request.QueryFunc(r, "hex", func(s string) (int64, error) {
		return strconv.ParseInt(s, 16, 64)
	})
	require.NoError(t, err)
	assert.Equal(t, int64(255), v)
}

func TestQuerySlice(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?id=1&id=2&id=3", nil)
	ids, err := request.QuerySlice[int](r, "id")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, ids)
}

func TestQuerySplit(t *testing.T) {
	t.Parallel()
	// decodes to: filter=orange, blue ,gray,
	r := httptest.NewRequest(http.MethodGet, "/?filter=orange,%20blue%20,gray,", nil)
	got, err := request.QuerySplit[string](r, "filter", ",")
	require.NoError(t, err)
	assert.Equal(t, []string{"orange", "blue", "gray"}, got)
}

func TestHasQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?x=", nil)
	assert.True(t, request.HasQuery(r, "x"))
	assert.False(t, request.HasQuery(r, "y"))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`undefined: request.Query`).

- [ ] **Step 3: Write the parse engine and shared resolvers**

Create `request/parse.go`:

```go
package request

import (
	"encoding"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parse converts a single non-empty raw string into T. Resolution order: built-in
// scalars via a type switch on any(&v); *time.Duration via time.ParseDuration; then
// any type whose pointer implements encoding.TextUnmarshaler (time.Time, uuid.UUID,
// netip.Addr, custom enums). No reflect package: interface assertions only.
func parse[T any](raw string) (T, error) {
	var v T
	switch p := any(&v).(type) {
	case *string:
		*p = raw
	case *bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return v, err
		}
		*p = b
	case *int:
		n, err := strconv.ParseInt(raw, 10, strconv.IntSize)
		if err != nil {
			return v, err
		}
		*p = int(n)
	case *int8:
		n, err := strconv.ParseInt(raw, 10, 8)
		if err != nil {
			return v, err
		}
		*p = int8(n)
	case *int16:
		n, err := strconv.ParseInt(raw, 10, 16)
		if err != nil {
			return v, err
		}
		*p = int16(n)
	case *int32:
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return v, err
		}
		*p = int32(n)
	case *int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return v, err
		}
		*p = n
	case *uint:
		n, err := strconv.ParseUint(raw, 10, strconv.IntSize)
		if err != nil {
			return v, err
		}
		*p = uint(n)
	case *uint8:
		n, err := strconv.ParseUint(raw, 10, 8)
		if err != nil {
			return v, err
		}
		*p = uint8(n)
	case *uint16:
		n, err := strconv.ParseUint(raw, 10, 16)
		if err != nil {
			return v, err
		}
		*p = uint16(n)
	case *uint32:
		n, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return v, err
		}
		*p = uint32(n)
	case *uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return v, err
		}
		*p = n
	case *float32:
		f, err := strconv.ParseFloat(raw, 32)
		if err != nil {
			return v, err
		}
		*p = float32(f)
	case *float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return v, err
		}
		*p = f
	case *time.Duration:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return v, err
		}
		*p = d
	default:
		if tu, ok := any(&v).(encoding.TextUnmarshaler); ok {
			if err := tu.UnmarshalText([]byte(raw)); err != nil {
				return v, err
			}
			return v, nil
		}
		return v, fmt.Errorf("unsupported type %T", v)
	}
	return v, nil
}

// resolve applies the missing/default/malformed contract for a single value: an
// empty raw string yields def[0] (or the zero value) and a nil error; a non-empty
// value is parsed by p and any failure is wrapped as *Error.
func resolve[T any](src Source, key, raw string, p func(string) (T, error), def []T) (T, error) {
	if raw == "" {
		if len(def) > 0 {
			return def[0], nil
		}
		var zero T
		return zero, nil
	}
	v, err := p(raw)
	if err != nil {
		var zero T
		return zero, &Error{Source: src, Key: key, Kind: KindMalformed, Err: err}
	}
	return v, nil
}

// resolveSlice parses each non-empty element of raws with p. Any failure wraps as
// *Error; an all-empty result falls back to def[0] (or nil).
func resolveSlice[T any](src Source, key string, raws []string, p func(string) (T, error), def [][]T) ([]T, error) {
	out := make([]T, 0, len(raws))
	for _, raw := range raws {
		if raw == "" {
			continue
		}
		v, err := p(raw)
		if err != nil {
			return nil, &Error{Source: src, Key: key, Kind: KindMalformed, Err: err}
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		if len(def) > 0 {
			return def[0], nil
		}
		return nil, nil
	}
	return out, nil
}

// resolveSplit splits raw on sep, trims and skips empty elements, then parses each
// with p. An empty raw falls back to def[0] (or nil).
func resolveSplit[T any](src Source, key, raw, sep string, p func(string) (T, error), def [][]T) ([]T, error) {
	if raw == "" {
		if len(def) > 0 {
			return def[0], nil
		}
		return nil, nil
	}
	var out []T
	for part := range strings.SplitSeq(raw, sep) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := p(part)
		if err != nil {
			return nil, &Error{Source: src, Key: key, Kind: KindMalformed, Err: err}
		}
		out = append(out, v)
	}
	return out, nil
}
```

Create `request/query.go`:

```go
package request

import "net/http"

// Query reads URL query parameter key and converts it to T. Absent/empty yields
// def[0] (or the zero value) with a nil error; a present unparseable value returns
// a *Error with Kind Malformed.
func Query[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceQuery, key, r.URL.Query().Get(key), parse[T], def)
}

// QueryFunc is Query with a caller-supplied parser instead of the built-in engine.
func QueryFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceQuery, key, r.URL.Query().Get(key), parse, def)
}

// QuerySlice reads every repeated value of key (?id=1&id=2) and parses each into T.
func QuerySlice[T any](r *http.Request, key string, def ...[]T) ([]T, error) {
	return resolveSlice(SourceQuery, key, r.URL.Query()[key], parse[T], def)
}

// QuerySliceFunc is QuerySlice with a caller-supplied element parser.
func QuerySliceFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	return resolveSlice(SourceQuery, key, r.URL.Query()[key], parse, def)
}

// QuerySplit reads a single delimited value (?filter=a,b,c), splitting on sep.
func QuerySplit[T any](r *http.Request, key, sep string, def ...[]T) ([]T, error) {
	return resolveSplit(SourceQuery, key, r.URL.Query().Get(key), sep, parse[T], def)
}

// QuerySplitFunc is QuerySplit with a caller-supplied element parser.
func QuerySplitFunc[T any](r *http.Request, key, sep string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	return resolveSplit(SourceQuery, key, r.URL.Query().Get(key), sep, parse, def)
}

// HasQuery reports whether key is present in the URL query (even with an empty value).
func HasQuery(r *http.Request, key string) bool {
	return r.URL.Query().Has(key)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add request/parse.go request/query.go request/query_test.go
git commit -m "feat(request): generic parse engine and Query accessor family"
```

---

### Task 3: Path, Header (+BearerToken), and Cookie families

**Files:**
- Create: `request/path.go`
- Create: `request/header.go`
- Create: `request/cookie.go`
- Test: `request/parts_test.go`

**Interfaces:**
- Consumes: `resolve`, `parse` (Task 2); `SourcePath`/`SourceHeader`/`SourceCookie` (Task 1).
- Produces:
  - `Path`, `PathFunc`, `HasPath`
  - `Header`, `HeaderFunc`, `HasHeader`, `func BearerToken(r *http.Request) (string, bool)`
  - `Cookie`, `CookieFunc`, `HasCookie`

- [ ] **Step 1: Write the failing test**

Create `request/parts_test.go`:

```go
package request_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPath(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/u/42", nil)
	r.SetPathValue("id", "42")

	v, err := request.Path[int](r, "id")
	require.NoError(t, err)
	assert.Equal(t, 42, v)
	assert.True(t, request.HasPath(r, "id"))
	assert.False(t, request.HasPath(r, "missing"))
}

func TestHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Count", "5")

	v, err := request.Header[int](r, "X-Count")
	require.NoError(t, err)
	assert.Equal(t, 5, v)
	assert.True(t, request.HasHeader(r, "X-Count"))
	assert.False(t, request.HasHeader(r, "X-Absent"))
}

func TestBearerToken(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer abc.def")
	tok, ok := request.BearerToken(r)
	assert.True(t, ok)
	assert.Equal(t, "abc.def", tok)

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("Authorization", "Basic xxx")
	_, ok2 := request.BearerToken(r2)
	assert.False(t, ok2)

	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	_, ok3 := request.BearerToken(r3)
	assert.False(t, ok3)
}

func TestCookie(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "sid", Value: "xyz"})

	v, err := request.Cookie[string](r, "sid")
	require.NoError(t, err)
	assert.Equal(t, "xyz", v)
	assert.True(t, request.HasCookie(r, "sid"))
	assert.False(t, request.HasCookie(r, "nope"))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`undefined: request.Path`, `request.BearerToken`).

- [ ] **Step 3: Write the three source families**

Create `request/path.go`:

```go
package request

import "net/http"

// Path reads path wildcard key (r.PathValue) and converts it to T. Requires a
// Go 1.22+ ServeMux pattern that defines key.
func Path[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourcePath, key, r.PathValue(key), parse[T], def)
}

// PathFunc is Path with a caller-supplied parser.
func PathFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourcePath, key, r.PathValue(key), parse, def)
}

// HasPath reports whether the path wildcard key is set and non-empty.
func HasPath(r *http.Request, key string) bool {
	return r.PathValue(key) != ""
}
```

Create `request/header.go`:

```go
package request

import (
	"net/http"
	"strings"
)

// Header reads request header key and converts it to T.
func Header[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceHeader, key, r.Header.Get(key), parse[T], def)
}

// HeaderFunc is Header with a caller-supplied parser.
func HeaderFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceHeader, key, r.Header.Get(key), parse, def)
}

// HasHeader reports whether header key is present and non-empty.
func HasHeader(r *http.Request, key string) bool {
	return r.Header.Get(key) != ""
}

// BearerToken returns the token from an Authorization: Bearer <token> header, or
// "" and false if the header is absent or not a Bearer credential.
func BearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(auth[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}
```

Create `request/cookie.go`:

```go
package request

import "net/http"

// Cookie reads cookie key's value and converts it to T. A missing cookie is treated
// as absent (zero value / default).
func Cookie[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceCookie, key, cookieValue(r, key), parse[T], def)
}

// CookieFunc is Cookie with a caller-supplied parser.
func CookieFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceCookie, key, cookieValue(r, key), parse, def)
}

// HasCookie reports whether cookie key is present.
func HasCookie(r *http.Request, key string) bool {
	_, err := r.Cookie(key)
	return err == nil
}

func cookieValue(r *http.Request, key string) string {
	c, err := r.Cookie(key)
	if err != nil {
		return ""
	}
	return c.Value
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add request/path.go request/header.go request/cookie.go request/parts_test.go
git commit -m "feat(request): path, header (with BearerToken), and cookie accessors"
```

---

### Task 4: Form accessor family

**Files:**
- Create: `request/form.go`
- Test: `request/form_test.go`

**Interfaces:**
- Consumes: `resolve`, `resolveSlice`, `resolveSplit`, `parse` (Task 2); `SourceForm` (Task 1).
- Produces:
  - `FormValue`, `FormValueFunc`, `FormSlice`, `FormSliceFunc`, `FormSplit`, `FormSplitFunc`, `HasForm`. All read the **body** form (POST/PUT/PATCH), never the URL query.

- [ ] **Step 1: Write the failing test**

Create `request/form_test.go`:

```go
package request_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func formReq(values url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestFormValue(t *testing.T) {
	t.Parallel()
	r := formReq(url.Values{"age": {"30"}})

	v, err := request.FormValue[int](r, "age")
	require.NoError(t, err)
	assert.Equal(t, 30, v)
	assert.True(t, request.HasForm(r, "age"))
	assert.False(t, request.HasForm(r, "missing"))
}

func TestFormSlice(t *testing.T) {
	t.Parallel()
	r := formReq(url.Values{"tag": {"a", "b"}})

	v, err := request.FormSlice[string](r, "tag")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, v)
}

func TestFormSplit(t *testing.T) {
	t.Parallel()
	r := formReq(url.Values{"ids": {"1, 2 ,3"}})

	v, err := request.FormSplit[int](r, "ids", ",")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, v)
}

func TestFormNotMergedWithQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/?age=99",
		strings.NewReader(url.Values{"age": {"30"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	v, err := request.FormValue[int](r, "age")
	require.NoError(t, err)
	assert.Equal(t, 30, v) // body value, not the query's 99
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`undefined: request.FormValue`).

- [ ] **Step 3: Write the form family**

Create `request/form.go`:

```go
package request

import "net/http"

// FormValue reads body form field key (POST/PUT/PATCH) and converts it to T. It
// reads only the request body, never the URL query.
func FormValue[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceForm, key, r.PostFormValue(key), parse[T], def)
}

// FormValueFunc is FormValue with a caller-supplied parser.
func FormValueFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceForm, key, r.PostFormValue(key), parse, def)
}

// FormSlice reads every repeated body value of key and parses each into T.
func FormSlice[T any](r *http.Request, key string, def ...[]T) ([]T, error) {
	_ = r.ParseForm()
	return resolveSlice(SourceForm, key, r.PostForm[key], parse[T], def)
}

// FormSliceFunc is FormSlice with a caller-supplied element parser.
func FormSliceFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	_ = r.ParseForm()
	return resolveSlice(SourceForm, key, r.PostForm[key], parse, def)
}

// FormSplit reads a single delimited body value, splitting on sep.
func FormSplit[T any](r *http.Request, key, sep string, def ...[]T) ([]T, error) {
	return resolveSplit(SourceForm, key, r.PostFormValue(key), sep, parse[T], def)
}

// FormSplitFunc is FormSplit with a caller-supplied element parser.
func FormSplitFunc[T any](r *http.Request, key, sep string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	return resolveSplit(SourceForm, key, r.PostFormValue(key), sep, parse, def)
}

// HasForm reports whether body form field key is present.
func HasForm(r *http.Request, key string) bool {
	_ = r.ParseForm()
	return r.PostForm.Has(key)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add request/form.go request/form_test.go
git commit -m "feat(request): body form accessor family"
```

---

### Task 5: Body options, DecodeJSON, and IsContentType

**Files:**
- Create: `request/body.go`
- Test: `request/body_test.go`

**Interfaces:**
- Consumes: `Error`, `SourceBody`, `KindTooLarge`, `KindUnsupportedMediaType`, `KindInvalidBody` (Task 1).
- Produces:
  - `type BodyOption func(*bodyConfig)`; `WithMaxBytes(int64)`, `AllowUnknownFields()`, `SkipContentType()`.
  - Internal `bodyConfig`, `newBodyConfig`, `limitedBody`, `matchesMediaType`, `decodeError`, and consts `defaultMaxBytes`/`defaultMultipartMemory` — reused by Task 6.
  - `func DecodeJSON(r *http.Request, dst any, opts ...BodyOption) error`
  - `func IsContentType(r *http.Request, media string) bool`

- [ ] **Step 1: Write the failing test**

Create `request/body_test.go`:

```go
package request_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type payload struct {
	Name string `json:"name"`
}

func jsonReq(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestDecodeJSONHappy(t *testing.T) {
	t.Parallel()
	var p payload
	require.NoError(t, request.DecodeJSON(jsonReq(`{"name":"ada"}`), &p))
	assert.Equal(t, "ada", p.Name)
}

func TestDecodeJSONUnknownField(t *testing.T) {
	t.Parallel()
	var p payload
	err := request.DecodeJSON(jsonReq(`{"name":"ada","extra":1}`), &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestDecodeJSONAllowUnknown(t *testing.T) {
	t.Parallel()
	var p payload
	require.NoError(t, request.DecodeJSON(jsonReq(`{"name":"ada","extra":1}`), &p, request.AllowUnknownFields()))
	assert.Equal(t, "ada", p.Name)
}

func TestDecodeJSONWrongContentType(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "text/plain")
	var p payload
	err := request.DecodeJSON(r, &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, request.StatusCode(err))
}

func TestDecodeJSONSkipContentType(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
	// no Content-Type set
	var p payload
	require.NoError(t, request.DecodeJSON(r, &p, request.SkipContentType()))
	assert.Equal(t, "x", p.Name)
}

func TestDecodeJSONTooLarge(t *testing.T) {
	t.Parallel()
	var p payload
	big := `{"name":"` + strings.Repeat("x", 2000) + `"}`
	err := request.DecodeJSON(jsonReq(big), &p, request.WithMaxBytes(100))
	require.Error(t, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, request.StatusCode(err))
}

func TestDecodeJSONTrailingData(t *testing.T) {
	t.Parallel()
	var p payload
	err := request.DecodeJSON(jsonReq(`{"name":"a"}{"name":"b"}`), &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestDecodeJSONEmpty(t *testing.T) {
	t.Parallel()
	var p payload
	err := request.DecodeJSON(jsonReq(``), &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestIsContentType(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Content-Type", "Application/JSON; charset=utf-8")
	assert.True(t, request.IsContentType(r, "application/json"))
	assert.False(t, request.IsContentType(r, "text/plain"))

	bare := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.False(t, request.IsContentType(bare, "application/json"))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`undefined: request.DecodeJSON`).

- [ ] **Step 3: Write the body options, decoder, and predicate**

Create `request/body.go`:

```go
package request

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	defaultMaxBytes        = 1 << 20  // 1 MiB — DecodeJSON / RawBody body cap
	defaultMultipartMemory = 32 << 20 // 32 MiB — File / Files in-memory cap
)

type bodyConfig struct {
	maxBytes     int64
	maxBytesSet  bool
	allowUnknown bool
	skipCType    bool
}

// BodyOption configures DecodeJSON, RawBody, File, and Files.
type BodyOption func(*bodyConfig)

// WithMaxBytes sets the body size cap for every body reader; n <= 0 disables the
// limit for DecodeJSON/RawBody (and falls back to 32 MiB for File/Files memory).
func WithMaxBytes(n int64) BodyOption {
	return func(c *bodyConfig) { c.maxBytes = n; c.maxBytesSet = true }
}

// AllowUnknownFields turns off DisallowUnknownFields (DecodeJSON only).
func AllowUnknownFields() BodyOption {
	return func(c *bodyConfig) { c.allowUnknown = true }
}

// SkipContentType accepts any/absent Content-Type (DecodeJSON only).
func SkipContentType() BodyOption {
	return func(c *bodyConfig) { c.skipCType = true }
}

func newBodyConfig(opts []BodyOption) bodyConfig {
	var c bodyConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}

// limitedBody wraps r.Body in a MaxBytesReader using the config's cap (default
// 1 MiB; a non-positive explicit cap disables the limit). The nil ResponseWriter
// is fine: MaxBytesReader only type-asserts w to signal the server, which we don't
// need here.
func limitedBody(r *http.Request, c bodyConfig) io.ReadCloser {
	limit := int64(defaultMaxBytes)
	if c.maxBytesSet {
		limit = c.maxBytes
	}
	if limit <= 0 {
		return r.Body
	}
	return http.MaxBytesReader(nil, r.Body, limit)
}

// matchesMediaType reports whether header's media type equals media
// (case-insensitive, parameters ignored).
func matchesMediaType(header, media string) bool {
	if header == "" {
		return false
	}
	got, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	want, _, err := mime.ParseMediaType(media)
	if err != nil {
		want = media
	}
	return strings.EqualFold(got, want)
}

// decodeError classifies a body read/decode failure: an *http.MaxBytesError maps
// to KindTooLarge, anything else to KindInvalidBody.
func decodeError(err error) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return &Error{Source: SourceBody, Kind: KindTooLarge, Err: err}
	}
	return &Error{Source: SourceBody, Kind: KindInvalidBody, Err: err}
}

// DecodeJSON strictly decodes the JSON request body into dst. Defaults: 1 MiB cap
// (-> 413), require Content-Type application/json (-> 415), DisallowUnknownFields
// and reject trailing data and empty bodies (-> 400). Override with options.
func DecodeJSON(r *http.Request, dst any, opts ...BodyOption) error {
	c := newBodyConfig(opts)

	if !c.skipCType && !matchesMediaType(r.Header.Get("Content-Type"), "application/json") {
		return &Error{
			Source: SourceBody,
			Kind:   KindUnsupportedMediaType,
			Err:    fmt.Errorf("content-type %q is not application/json", r.Header.Get("Content-Type")),
		}
	}

	dec := json.NewDecoder(limitedBody(r, c))
	if !c.allowUnknown {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	if dec.More() {
		return &Error{Source: SourceBody, Kind: KindInvalidBody, Err: errors.New("unexpected data after JSON value")}
	}
	return nil
}

// IsContentType reports whether the request's Content-Type media type equals media
// (case-insensitive, parameters ignored). It inspects the request's own
// Content-Type, not the Accept header — it is not content negotiation.
func IsContentType(r *http.Request, media string) bool {
	return matchesMediaType(r.Header.Get("Content-Type"), media)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add request/body.go request/body_test.go
git commit -m "feat(request): strict DecodeJSON, body options, and IsContentType"
```

---

### Task 6: RawBody and multipart File / Files

**Files:**
- Modify: `request/body.go` (replace the import block; append `RawBody`, `File`, `Files`, `parseMultipart`)
- Test: `request/body_files_test.go`

**Interfaces:**
- Consumes: `newBodyConfig`, `limitedBody`, `decodeError`, `defaultMultipartMemory` (Task 5); `Error`, `SourceBody`, `SourceForm`, `KindMalformed`, `KindUnsupportedMediaType`, `KindInvalidBody` (Tasks 1, 5).
- Produces:
  - `func RawBody(r *http.Request, opts ...BodyOption) ([]byte, error)`
  - `func File(r *http.Request, key string, opts ...BodyOption) (multipart.File, *multipart.FileHeader, error)`
  - `func Files(r *http.Request, key string, opts ...BodyOption) ([]*multipart.FileHeader, error)`

- [ ] **Step 1: Write the failing test**

Create `request/body_files_test.go`:

```go
package request_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func multipartReq(t *testing.T, field, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = fw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	r := httptest.NewRequest(http.MethodPost, "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func TestRawBody(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	b, err := request.RawBody(r)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))
}

func TestRawBodyTooLarge(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789"))
	_, err := request.RawBody(r, request.WithMaxBytes(4))
	require.Error(t, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, request.StatusCode(err))
}

func TestFile(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "doc", "a.txt", "filedata")

	f, h, err := request.File(r, "doc")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	assert.Equal(t, "a.txt", h.Filename)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "filedata", string(data))
}

func TestFileMissing(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "other", "x.txt", "x")
	_, _, err := request.File(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestFileNonMultipart(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	r.Header.Set("Content-Type", "application/json")
	_, _, err := request.File(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, request.StatusCode(err))
}

func TestFiles(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "doc", "a.txt", "data")
	headers, err := request.Files(r, "doc")
	require.NoError(t, err)
	require.Len(t, headers, 1)
	assert.Equal(t, "a.txt", headers[0].Filename)
}

func TestFilesMissing(t *testing.T) {
	t.Parallel()
	r := multipartReq(t, "other", "x.txt", "x")
	_, err := request.Files(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestFilesNonMultipart(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	r.Header.Set("Content-Type", "application/json")
	_, err := request.Files(r, "doc")
	require.Error(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, request.StatusCode(err))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`undefined: request.RawBody`, `request.File`).

- [ ] **Step 3: Add the multipart import and the three functions**

Replace the import block at the top of `request/body.go` with (adds `mime/multipart`):

```go
import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)
```

Append to `request/body.go`:

```go
// RawBody reads the entire request body into a byte slice under the configured
// size cap (default 1 MiB; overflow -> 413). For webhook HMAC verification and
// arbitrary payloads. No Content-Type check.
func RawBody(r *http.Request, opts ...BodyOption) ([]byte, error) {
	c := newBodyConfig(opts)
	b, err := io.ReadAll(limitedBody(r, c))
	if err != nil {
		return nil, decodeError(err)
	}
	return b, nil
}

// File returns the first uploaded file for key. The caller must Close the returned
// multipart.File. A non-multipart request -> 415; a missing file -> 400.
func File(r *http.Request, key string, opts ...BodyOption) (multipart.File, *multipart.FileHeader, error) {
	if err := parseMultipart(r, opts); err != nil {
		return nil, nil, err
	}
	f, h, err := r.FormFile(key)
	if err != nil {
		return nil, nil, &Error{Source: SourceForm, Key: key, Kind: KindMalformed, Err: err}
	}
	return f, h, nil
}

// Files returns every uploaded file header for key (open each via fh.Open()).
func Files(r *http.Request, key string, opts ...BodyOption) ([]*multipart.FileHeader, error) {
	if err := parseMultipart(r, opts); err != nil {
		return nil, err
	}
	var headers []*multipart.FileHeader
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		headers = r.MultipartForm.File[key]
	}
	if len(headers) == 0 {
		return nil, &Error{Source: SourceForm, Key: key, Kind: KindMalformed, Err: errors.New("no file for key")}
	}
	return headers, nil
}

// parseMultipart parses the multipart form, using the configured in-memory cap
// (default 32 MiB). A non-multipart body maps to KindUnsupportedMediaType.
func parseMultipart(r *http.Request, opts []BodyOption) error {
	c := newBodyConfig(opts)
	mem := int64(defaultMultipartMemory)
	if c.maxBytesSet && c.maxBytes > 0 {
		mem = c.maxBytes
	}
	if err := r.ParseMultipartForm(mem); err != nil {
		kind := KindInvalidBody
		if errors.Is(err, http.ErrNotMultipart) {
			kind = KindUnsupportedMediaType
		}
		return &Error{Source: SourceBody, Kind: kind, Err: err}
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add request/body.go request/body_files_test.go
git commit -m "feat(request): RawBody and multipart File/Files readers"
```

---

### Task 7: ClientIP — real-client-IP resolution

**Files:**
- Create: `request/clientip.go`
- Test: `request/clientip_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (uses `net`, `net/http`, `net/netip`, `strings`).
- Produces:
  - `type ClientIPOption func(*clientIPConfig)`; `WithClientIPHeaders(names ...string)`, `WithTrustedProxies(prefixes ...netip.Prefix)`.
  - `func ClientIP(r *http.Request, opts ...ClientIPOption) string`

- [ ] **Step 1: Write the failing test**

Create `request/clientip_test.go`:

```go
package request_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
)

func TestClientIPPriority(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "203.0.113.5")
	r.Header.Set("CF-Connecting-IP", "198.51.100.7")
	assert.Equal(t, "198.51.100.7", request.ClientIP(r)) // CF outranks X-Real-IP
}

func TestClientIPFallbackRemoteAddr(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.9:5555"
	assert.Equal(t, "192.0.2.9", request.ClientIP(r))
}

func TestClientIPXFFFirstValid(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "garbage, 203.0.113.5, 70.41.3.18")
	assert.Equal(t, "203.0.113.5", request.ClientIP(r))
}

func TestClientIPForwarded(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("Forwarded", `for=192.0.2.60;proto=http, for=198.51.100.17`)
	assert.Equal(t, "192.0.2.60", request.ClientIP(r))
}

func TestClientIPPinnedHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("CF-Connecting-IP", "9.9.9.9")
	// pin to a header that is absent -> ignore XFF/CF, fall back to RemoteAddr
	assert.Equal(t, "10.0.0.1", request.ClientIP(r, request.WithClientIPHeaders("X-Real-IP")))
}

func TestClientIPTrustedProxies(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1" // trusted hop
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.2")
	trusted := netip.MustParsePrefix("10.0.0.0/8")
	assert.Equal(t, "203.0.113.5", request.ClientIP(r, request.WithTrustedProxies(trusted)))
}

func TestClientIPMalformedRemoteAddr(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "not-an-ip" // no usable headers, unparseable peer
	assert.Equal(t, "", request.ClientIP(r))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`undefined: request.ClientIP`).

- [ ] **Step 3: Write the resolver**

Create `request/clientip.go`:

```go
package request

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

type clientIPConfig struct {
	headers    []string
	trusted    []netip.Prefix
	useTrusted bool
}

// ClientIPOption configures ClientIP.
type ClientIPOption func(*clientIPConfig)

// WithClientIPHeaders replaces the header priority list scanned in best-effort
// mode. The secure pattern for a known CDN is to pin the single header the edge
// always sets and strips on ingress, e.g. WithClientIPHeaders("CF-Connecting-IP").
func WithClientIPHeaders(names ...string) ClientIPOption {
	return func(c *clientIPConfig) { c.headers = names }
}

// WithTrustedProxies switches to spoof-resistant X-Forwarded-For resolution: the
// chain (XFF entries plus RemoteAddr) is walked right-to-left, trusted hops are
// skipped, and the first untrusted address is returned.
func WithTrustedProxies(prefixes ...netip.Prefix) ClientIPOption {
	return func(c *clientIPConfig) { c.trusted = prefixes; c.useTrusted = true }
}

// ClientIP returns the best guess at the originating client IP, or "" if nothing
// parses. By default it scans well-known CDN/proxy headers in priority order and
// falls back to RemoteAddr. The default mode trusts client-supplied headers and is
// spoofable; use WithTrustedProxies or a pinned header for auth/rate-limiting.
func ClientIP(r *http.Request, opts ...ClientIPOption) string {
	// Best-effort scan order. Trusting these is spoofable unless the service sits
	// behind a proxy that overwrites them.
	c := clientIPConfig{headers: []string{
		"CF-Connecting-IP",
		"True-Client-IP",
		"Fastly-Client-IP",
		"X-Real-IP",
		"Forwarded",
		"X-Forwarded-For",
	}}
	for _, o := range opts {
		o(&c)
	}

	if c.useTrusted {
		return clientIPTrusted(r, c.trusted)
	}
	for _, name := range c.headers {
		if ip := headerIP(r, name); ip != "" {
			return ip
		}
	}
	return remoteHost(r.RemoteAddr)
}

// headerIP extracts the first valid IP from header name. Forwarded uses RFC 7239
// for= parsing; every other header is treated as a comma list, first valid wins.
func headerIP(r *http.Request, name string) string {
	raw := r.Header.Get(name)
	if raw == "" {
		return ""
	}
	if strings.EqualFold(name, "Forwarded") {
		return forwardedFor(raw)
	}
	for part := range strings.SplitSeq(raw, ",") {
		if ip := validIP(part); ip != "" {
			return ip
		}
	}
	return ""
}

// clientIPTrusted walks the XFF chain (then RemoteAddr) right-to-left and returns
// the first address not inside a trusted prefix. If all hops are trusted, the
// left-most chain entry (closest to the originating client) is returned.
func clientIPTrusted(r *http.Request, trusted []netip.Prefix) string {
	var chain []string
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		chain = strings.Split(xff, ",")
	}
	chain = append(chain, r.RemoteAddr)

	for _, hop := range slices.Backward(chain) {
		ip := validIP(hop)
		if ip == "" {
			continue
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		if isTrusted(addr, trusted) {
			continue
		}
		return ip
	}
	if len(chain) > 0 {
		if ip := validIP(chain[0]); ip != "" {
			return ip
		}
	}
	return remoteHost(r.RemoteAddr)
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// forwardedFor returns the IP from the first "for=" directive of a Forwarded header.
func forwardedFor(v string) string {
	first, _, _ := strings.Cut(v, ",")
	for kv := range strings.SplitSeq(first, ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
			continue
		}
		return validIP(strings.Trim(strings.TrimSpace(val), `"`))
	}
	return ""
}

// validIP normalizes s (which may carry a port or brackets) to a bare IP string,
// or "" if it is not a valid address.
func validIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.Trim(s, "[]")
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return ""
	}
	return addr.String()
}

// remoteHost returns the IP from a RemoteAddr ("ip:port" or bare ip), or "" if it
// does not parse as an IP.
func remoteHost(addr string) string {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a.String()
	}
	return ""
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add request/clientip.go request/clientip_test.go
git commit -m "feat(request): ClientIP with CDN/proxy header resolution"
```

---

### Task 8: Pagination — page and cursor

**Files:**
- Create: `request/pagination.go`
- Test: `request/pagination_test.go`

**Interfaces:**
- Consumes: `Query` (Task 2) for reading and malformed-error propagation.
- Produces:
  - `type Page struct { Number, Size, Offset int }`; `type Cursor struct { Value string; Limit int }`.
  - `type PageOption func(*pageConfig)`; `WithPageParams(pageKey, sizeKey string)`, `WithDefaultPageSize(n int)`, `WithMaxPageSize(n int)`, `WithCursorParams(cursorKey, limitKey string)`.
  - `func QueryPage(r *http.Request, opts ...PageOption) (Page, error)`
  - `func QueryCursor(r *http.Request, opts ...PageOption) (Cursor, error)`

- [ ] **Step 1: Write the failing test**

Create `request/pagination_test.go`:

```go
package request_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryPageDefaults(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	p, err := request.QueryPage(r)
	require.NoError(t, err)
	assert.Equal(t, 1, p.Number)
	assert.Equal(t, 20, p.Size)
	assert.Equal(t, 0, p.Offset)
}

func TestQueryPageValues(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?page=3&per_page=10", nil)
	p, err := request.QueryPage(r)
	require.NoError(t, err)
	assert.Equal(t, 3, p.Number)
	assert.Equal(t, 10, p.Size)
	assert.Equal(t, 20, p.Offset)
}

func TestQueryPageClamp(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?page=0&per_page=9999", nil)
	p, err := request.QueryPage(r, request.WithMaxPageSize(100))
	require.NoError(t, err)
	assert.Equal(t, 1, p.Number)   // clamped up to 1
	assert.Equal(t, 100, p.Size)   // clamped down to max
}

func TestQueryPageMalformed(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?page=abc", nil)
	_, err := request.QueryPage(r)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestQueryPageCustomParams(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?p=2&size=5", nil)
	p, err := request.QueryPage(r, request.WithPageParams("p", "size"))
	require.NoError(t, err)
	assert.Equal(t, 2, p.Number)
	assert.Equal(t, 5, p.Size)
}

func TestQueryCursor(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?cursor=abc123&limit=5", nil)
	c, err := request.QueryCursor(r)
	require.NoError(t, err)
	assert.Equal(t, "abc123", c.Value)
	assert.Equal(t, 5, c.Limit)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./request/`
Expected: FAIL — build error (`undefined: request.QueryPage`).

- [ ] **Step 3: Write the pagination readers**

Create `request/pagination.go`:

```go
package request

import "net/http"

// Page is offset-based pagination input. Offset is derived as (Number-1)*Size.
type Page struct {
	Number int
	Size   int
	Offset int
}

// Cursor is cursor-based pagination input. Value is an opaque token passed through
// verbatim; decoding its payload is the caller's job.
type Cursor struct {
	Value string
	Limit int
}

type pageConfig struct {
	pageKey     string
	sizeKey     string
	cursorKey   string
	limitKey    string
	defaultSize int
	maxSize     int
}

// PageOption configures QueryPage and QueryCursor.
type PageOption func(*pageConfig)

// WithPageParams sets the query parameter names for page number and size.
func WithPageParams(pageKey, sizeKey string) PageOption {
	return func(c *pageConfig) { c.pageKey = pageKey; c.sizeKey = sizeKey }
}

// WithCursorParams sets the query parameter names for cursor and limit.
func WithCursorParams(cursorKey, limitKey string) PageOption {
	return func(c *pageConfig) { c.cursorKey = cursorKey; c.limitKey = limitKey }
}

// WithDefaultPageSize sets the size/limit used when the parameter is absent.
func WithDefaultPageSize(n int) PageOption {
	return func(c *pageConfig) { c.defaultSize = n }
}

// WithMaxPageSize sets the upper bound that size/limit is clamped to.
func WithMaxPageSize(n int) PageOption {
	return func(c *pageConfig) { c.maxSize = n }
}

func newPageConfig(opts []PageOption) pageConfig {
	c := pageConfig{
		pageKey:     "page",
		sizeKey:     "per_page",
		cursorKey:   "cursor",
		limitKey:    "limit",
		defaultSize: 20,
		maxSize:     100,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// QueryPage reads offset-based pagination from the query. A non-numeric value is a
// Malformed error (-> 400); a valid out-of-range value is clamped (page >= 1,
// 1 <= size <= max).
func QueryPage(r *http.Request, opts ...PageOption) (Page, error) {
	c := newPageConfig(opts)

	number, err := Query[int](r, c.pageKey, 1)
	if err != nil {
		return Page{}, err
	}
	size, err := Query[int](r, c.sizeKey, c.defaultSize)
	if err != nil {
		return Page{}, err
	}
	number = max(number, 1)
	size = min(max(size, 1), c.maxSize)
	return Page{Number: number, Size: size, Offset: (number - 1) * size}, nil
}

// QueryCursor reads cursor-based pagination from the query. The cursor is opaque;
// limit shares the same default/max bounds as page size.
func QueryCursor(r *http.Request, opts ...PageOption) (Cursor, error) {
	c := newPageConfig(opts)

	limit, err := Query[int](r, c.limitKey, c.defaultSize)
	if err != nil {
		return Cursor{}, err
	}
	value, err := Query[string](r, c.cursorKey)
	if err != nil {
		return Cursor{}, err
	}
	return Cursor{Value: value, Limit: min(max(limit, 1), c.maxSize)}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./request/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add request/pagination.go request/pagination_test.go
git commit -m "feat(request): page and cursor pagination readers"
```

---

### Task 9: Runnable examples, benchmark, and final verification

**Files:**
- Create: `request/example_test.go`

**Interfaces:**
- Consumes: `Query`, `DecodeJSON`, `StatusCode` (Tasks 1, 2, 5).
- Produces: nothing (godoc examples + benchmark only).

- [ ] **Step 1: Write the examples and benchmark**

Create `request/example_test.go`:

```go
package request_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/request"
)

func ExampleQuery() {
	r := httptest.NewRequest(http.MethodGet, "/search?page=2", nil)
	page, _ := request.Query[int](r, "page", 1)
	fmt.Println(page)
	// Output: 2
}

func ExampleDecodeJSON() {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ada"}`))
	r.Header.Set("Content-Type", "application/json")

	var v struct {
		Name string `json:"name"`
	}
	if err := request.DecodeJSON(r, &v); err != nil {
		fmt.Println("status:", request.StatusCode(err))
		return
	}
	fmt.Println(v.Name)
	// Output: ada
}

func BenchmarkQueryInt(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/?n=12345", nil)
	b.ReportAllocs()
	for range b.N {
		_, _ = request.Query[int](r, "n")
	}
}
```

- [ ] **Step 2: Run the examples to verify they pass**

Run: `just test ./request/`
Expected: PASS (Go runs `Example*` functions and checks their `// Output:` blocks).

- [ ] **Step 3: Run the full check (fmt + lint + test)**

Run: `just check`
Expected: `go fmt`/`goimports`/`betteralign` make no changes; `go vet`, `golangci-lint`, `nilaway`, `betteralign`, `modernize` report nothing; all tests pass. If `betteralign -apply` (run by `just fmt`) reorders any struct field (e.g. in `bodyConfig`, `clientIPConfig`, `pageConfig`, `Error`), re-run `just test ./request/` — all struct literals in the package use named fields, so reordering is safe — and include the change in the commit below.

- [ ] **Step 4: Commit**

```bash
git add request/example_test.go
git commit -m "docs(request): runnable examples and parse benchmark"
```

---

## Self-Review

**1. Spec coverage** — every spec section maps to a task:

- Generic `parse[T]` engine (scalars + `time.Duration` + `TextUnmarshaler`) → Task 2 (`parse.go`).
- Missing/default/malformed contract (`resolve`, `resolveSlice`, `resolveSplit`) → Task 2; reused by Tasks 3–4.
- Scalar accessors + `…Func` + `…Slice` + `…Split` for Query → Task 2; Form → Task 4; scalar-only Path/Header/Cookie → Task 3.
- Presence predicates (`HasQuery`/`HasPath`/`HasHeader`/`HasCookie`/`HasForm`) → Tasks 2–4 (one per source file).
- `BearerToken` → Task 3 (`header.go`).
- Strict `DecodeJSON` (1 MiB→413, Content-Type→415, unknown-field/trailing/empty→400) + `BodyOption`/`WithMaxBytes`/`AllowUnknownFields`/`SkipContentType` → Task 5.
- `IsContentType` → Task 5.
- `RawBody` + multipart `File`/`Files` → Task 6.
- `ClientIP` best-effort priority scan + `WithClientIPHeaders` pin + `WithTrustedProxies` right-to-left → Task 7.
- Pagination `Page`/`Cursor`/`QueryPage`/`QueryCursor` + `PageOption` family, clamp-vs-malformed → Task 8.
- Error model (`Source`/`Kind`/`*Error`/`StatusCode`) → Task 1.
- `doc.go` package overview → Task 1; runnable Examples + `parse` benchmark → Task 9.
- Black-box `package request_test`, `httptest`-driven, testify → all tasks.

**2. Placeholder scan** — no TBD/TODO/"add error handling"/"similar to Task N"; every code step shows complete code. ✔

**3. Type consistency** — the option types match their consumers: `BodyOption` (Tasks 5–6), `ClientIPOption` (Task 7), `PageOption` (Task 8). `*Error{Source, Key, Kind, Err}` field names are identical across `errors.go`, the resolvers, and every accessor. The generic accessor signature `[T any](r *http.Request, key string, def ...T) (T, error)` and the `…Func` shape `(…, parse func(string) (T, error), …)` are uniform across `query.go`/`path.go`/`header.go`/`cookie.go`/`form.go`. `resolve`/`resolveSlice`/`resolveSplit` signatures match their call sites in Tasks 2–4. ✔

