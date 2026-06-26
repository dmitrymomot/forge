# htmx Conveniences Extension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-add the useful conveniences the v1 `pkg/htmx` package had and the v2 rewrite dropped, plus an explicit external-redirect helper and a generic multi-fragment renderer, without reopening the rewrite's boundaries.

**Architecture:** Three new free functions and one new type land in the existing `htmx` package (`response.go`), which stays headers-only and stdlib-only. One generic helper (`Components`) and one sentinel (`ErrNoComponents`) land in the existing `render` package, which stays HTMX-agnostic. Out-of-band swaps are handled by documentation (templ composition + `render.Components`), not by new feature code. TDD throughout, one committed task per deliverable.

**Tech Stack:** Go 1.26, standard library only (`encoding/json`, `fmt`, `net/http`, `net/url`, `strings`, `context`), testify in tests, `just` task runner.

## Global Constraints

- Module path: `github.com/dmitrymomot/forge`; Go 1.26.
- `htmx` and `render` are **stdlib-only** — no external runtime dependencies (testify is test-only).
- **Black-box tests only:** test files are `package htmx_test` / `package render_test` and exercise only the exported surface.
- Errors are **single-line, package-prefixed, wrapped** (e.g. `errors.New("render: no components")`, `fmt.Errorf("render: render component: %w", err)`).
- **No builder pattern, no global mutable state** — free functions and plain values.
- Flat package layout; files split by responsibility.
- Run recipes via `just`: `just test <path>` (= `go clean -testcache && go test -race -cover <path>`), `just lint`, `just fmt`, `just check` (fmt+lint+test). For a single failing test during a micro-cycle, use `go test <path> -run '^TestName$' -v` directly.
- Conventional-commit messages with a package scope, e.g. `feat(htmx): ...`, `test(render): ...`, `docs(htmx): ...`.
- Source spec: `docs/superpowers/specs/2026-06-26-htmx-extend-conveniences-design.md`.

---

## File Structure

**Modified:**
- `htmx/response.go` — add `type Swap string` (retype the `Swap*` constants, `Reswap`, `LocationOptions.Swap`, `locationPayload.Swap`); add `RedirectExternal`, `RedirectBack`, `RedirectBackParam`, `LocationTarget`, internal `safeLocalPath` and `defaultRedirectParam`. New imports: `net/url`, `strings`.
- `htmx/response_test.go` — new black-box tests for all of the above. New import: `net/url`.
- `htmx/doc.go` — document the redirect helpers and add an out-of-band (OOB) section.
- `htmx/example_test.go` — add `ExampleRedirectBack` and `ExampleRedirectExternal`.
- `render/errors.go` — add `ErrNoComponents`.
- `render/doc.go` — mention `Components`.

**Created:**
- `render/components.go` — `func Components(ctx, w, status, ...Component) error`.
- `render/components_test.go` — black-box tests (reuse `fakeComponent`/`testCtxKey` from `render/templ_test.go`, same test package).

---

## Task 1: Typed `Swap`

Retype the swap constants into a named `Swap` type and thread it through `Reswap` and the location payload. This is a type-safety refactor; existing tests already use the constants in assignable positions, so they keep compiling.

**Files:**
- Modify: `htmx/response.go` (constants block, `Reswap`, `LocationOptions.Swap`, `locationPayload.Swap`)
- Test: `htmx/response_test.go`

**Interfaces:**
- Consumes: existing `hdrReswap` constant; existing `Reswap`/`LocationOptions`/`locationPayload`.
- Produces: `type Swap string`; typed constants `SwapInnerHTML … SwapNone Swap`; `Reswap(w http.ResponseWriter, swap Swap)`; `LocationOptions.Swap Swap`.

- [ ] **Step 1: Write the failing test**

Add to `htmx/response_test.go`:

```go
func TestSwapTypeIsTyped(t *testing.T) {
	t.Parallel()

	var s htmx.Swap = htmx.SwapInnerHTML // compile-time: the Swap* constants are typed Swap
	assert.Equal(t, htmx.Swap("innerHTML"), s)
}

func TestReswapModifierStaysTyped(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Reswap(rec, htmx.SwapInnerHTML+" swap:1s") // untyped string constant added to a Swap stays a Swap
	assert.Equal(t, "innerHTML swap:1s", rec.Header().Get("HX-Reswap"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./htmx/ -run '^TestSwapTypeIsTyped$' -v`
Expected: FAIL — build error `undefined: htmx.Swap` (the whole test package fails to compile until the type exists).

- [ ] **Step 3: Add the `Swap` type and retype the constants**

In `htmx/response.go`, replace the existing swap constants block:

```go
// Swap styles for Reswap and LocationOptions.Swap (the hx-swap values).
const (
	SwapInnerHTML   = "innerHTML"
	SwapOuterHTML   = "outerHTML"
	SwapTextContent = "textContent"
	SwapBeforeBegin = "beforebegin"
	SwapAfterBegin  = "afterbegin"
	SwapBeforeEnd   = "beforeend"
	SwapAfterEnd    = "afterend"
	SwapDelete      = "delete"
	SwapNone        = "none"
)
```

with:

```go
// Swap is an hx-swap style (the HX-Reswap value and LocationOptions.Swap). Use a Swap*
// constant; a modifier reads naturally as SwapInnerHTML + " swap:1s" (an untyped string
// constant added to a Swap stays a Swap, so no conversion is needed).
type Swap string

// Swap styles for Reswap and LocationOptions.Swap (the hx-swap values).
const (
	SwapInnerHTML   Swap = "innerHTML"
	SwapOuterHTML   Swap = "outerHTML"
	SwapTextContent Swap = "textContent"
	SwapBeforeBegin Swap = "beforebegin"
	SwapAfterBegin  Swap = "afterbegin"
	SwapBeforeEnd   Swap = "beforeend"
	SwapAfterEnd    Swap = "afterend"
	SwapDelete      Swap = "delete"
	SwapNone        Swap = "none"
)
```

- [ ] **Step 4: Retype `Reswap`**

In `htmx/response.go`, replace:

```go
func Reswap(w http.ResponseWriter, swap string) {
	w.Header().Set(hdrReswap, swap)
}
```

with:

```go
func Reswap(w http.ResponseWriter, swap Swap) {
	w.Header().Set(hdrReswap, string(swap))
}
```

- [ ] **Step 5: Retype the `Swap` field on both structs**

In `htmx/response.go`, in `LocationOptions` change the `Swap` field from `string` to `Swap`:

```go
	Swap    Swap              // how to swap (a Swap* value)
```

and in `locationPayload` change the `Swap` field from `string` to `Swap` (keep the json tag):

```go
	Swap    Swap              `json:"swap,omitempty"`
```

- [ ] **Step 6: Run the htmx tests to verify they pass**

Run: `just test ./htmx/`
Expected: PASS — all existing tests plus the two new ones (`omitempty` still drops an empty `Swap` because `Swap("")` is the zero value).

- [ ] **Step 7: Confirm nothing else in the repo broke**

Run: `go build ./...`
Expected: builds clean (a repo-wide search found no consumer of the swap API outside `htmx`).

- [ ] **Step 8: Commit**

```bash
git add htmx/response.go htmx/response_test.go
git commit -m "feat(htmx): make Swap a named type"
```

---

## Task 2: `RedirectExternal`

An explicit off-site redirect (full-page `HX-Redirect`, no URL validation) — the deliberate counterpart to `RedirectBack`'s local-only safety. It delegates to `Redirect` so there is no duplicated branch logic.

**Files:**
- Modify: `htmx/response.go` (add function after `Redirect`)
- Test: `htmx/response_test.go`

**Interfaces:**
- Consumes: existing `Redirect(w, r, url string, status ...int)`; the existing `htmxRequest()` test helper.
- Produces: `RedirectExternal(w http.ResponseWriter, r *http.Request, url string, status ...int)`.

- [ ] **Step 1: Write the failing tests**

Add to `htmx/response_test.go`:

```go
func TestRedirectExternalHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.RedirectExternal(rec, htmxRequest(), "https://example.com/oauth")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://example.com/oauth", rec.Header().Get("HX-Redirect")) // full-page nav, cross-origin safe
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestRedirectExternalNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.RedirectExternal(rec, r, "https://example.com/oauth")

	assert.Equal(t, http.StatusSeeOther, rec.Code) // default fallback 303
	assert.Equal(t, "https://example.com/oauth", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./htmx/ -run '^TestRedirectExternal' -v`
Expected: FAIL — build error `undefined: htmx.RedirectExternal`.

- [ ] **Step 3: Implement `RedirectExternal`**

In `htmx/response.go`, add immediately after the `Redirect` function:

```go
// RedirectExternal performs a full-page client redirect to an external (off-site) URL.
// It is the explicit counterpart to RedirectBack's local-only safety: it does NOT
// validate url, so reserve it for developer-controlled destinations, never raw user
// input. For HTMX requests it uses HX-Redirect (a full window.location navigation, which
// works cross-origin); for non-HTMX requests it falls back to http.Redirect with the
// optional status (default 303 See Other). Terminal — call it last.
//
// Use this, not Location/LocationTarget/LocationWith, for external URLs: those use
// HX-Location, an AJAX swap that is same-origin only and fails on a cross-origin target.
func RedirectExternal(w http.ResponseWriter, r *http.Request, url string, status ...int) {
	Redirect(w, r, url, status...)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./htmx/ -run '^TestRedirectExternal' -v`
Expected: PASS (both cases).

- [ ] **Step 5: Commit**

```bash
git add htmx/response.go htmx/response_test.go
git commit -m "feat(htmx): add RedirectExternal for explicit off-site redirects"
```

---

## Task 3: `RedirectBack` / `RedirectBackParam`

HTMX-aware redirect to a return path from a query parameter, honored only when it is a safe local path (open-redirect protection); otherwise a fallback.

**Files:**
- Modify: `htmx/response.go` (add `defaultRedirectParam`, `RedirectBack`, `RedirectBackParam`, `safeLocalPath`; add imports `net/url`, `strings`)
- Test: `htmx/response_test.go` (add import `net/url`)

**Interfaces:**
- Consumes: existing `Redirect(w, r, url string, status ...int)`.
- Produces: `RedirectBack(w http.ResponseWriter, r *http.Request, fallback string, status ...int)`; `RedirectBackParam(w http.ResponseWriter, r *http.Request, param, fallback string, status ...int)`.

- [ ] **Step 1: Write the failing tests**

Add `"net/url"` to the import block of `htmx/response_test.go`, then add:

```go
func TestRedirectBackHonorsSafeLocal(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login?redirect=/dashboard", nil)
	htmx.RedirectBack(rec, r, "/home")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
}

func TestRedirectBackHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login?redirect=/dashboard", nil)
	r.Header.Set("HX-Request", "true")
	htmx.RedirectBack(rec, r, "/home")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("HX-Redirect"))
}

func TestRedirectBackFallsBackOnUnsafeTarget(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, target string }{
		{"absolute", "https://evil.com"},
		{"protocol relative", "//evil.com"},
		{"backslash", "/\\evil.com"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/login?redirect="+url.QueryEscape(tc.target), nil)
			htmx.RedirectBack(rec, r, "/home")
			assert.Equal(t, "/home", rec.Header().Get("Location"))
		})
	}
}

func TestRedirectBackFallsBackWhenParamMissing(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login", nil)
	htmx.RedirectBack(rec, r, "/home")

	assert.Equal(t, "/home", rec.Header().Get("Location"))
}

func TestRedirectBackParamCustomName(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login?next=/dashboard", nil)
	htmx.RedirectBackParam(rec, r, "next", "/home")

	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./htmx/ -run '^TestRedirectBack' -v`
Expected: FAIL — build error `undefined: htmx.RedirectBack` / `htmx.RedirectBackParam`.

- [ ] **Step 3: Add the imports to `response.go`**

In `htmx/response.go`, expand the import block to include `net/url` and `strings`:

```go
import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)
```

- [ ] **Step 4: Implement the functions**

In `htmx/response.go`, add (place after the `Redirect`/`RedirectExternal` group):

```go
// defaultRedirectParam is the query parameter RedirectBack reads for the return path.
const defaultRedirectParam = "redirect"

// RedirectBack redirects to the local path in the "redirect" query parameter, or to
// fallback when that parameter is absent, empty, or not a safe local path. It is
// HTMX-aware (delegates to Redirect): HX-Redirect + 200 for HTMX requests, a real 3xx
// otherwise. The optional status sets the non-HTMX fallback code (only the first value
// is used), defaulting to http.StatusSeeOther (303). Terminal — call it last.
func RedirectBack(w http.ResponseWriter, r *http.Request, fallback string, status ...int) {
	RedirectBackParam(w, r, defaultRedirectParam, fallback, status...)
}

// RedirectBackParam is RedirectBack reading a caller-named query parameter instead of
// the default "redirect".
func RedirectBackParam(w http.ResponseWriter, r *http.Request, param, fallback string, status ...int) {
	target := r.URL.Query().Get(param)
	if !safeLocalPath(target) {
		target = fallback
	}
	Redirect(w, r, target, status...)
}

// safeLocalPath reports whether s is a relative, same-origin path safe to redirect to:
// it must begin with a single "/" (not "//", which is protocol-relative, and not "/\",
// which some browsers treat as protocol-relative) and parse to a URL with no scheme and
// no host. This blocks open redirects to attacker-controlled external destinations.
func safeLocalPath(s string) bool {
	if s == "" || s[0] != '/' {
		return false
	}
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "" && u.Host == ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./htmx/ -run '^TestRedirectBack' -v`
Expected: PASS (all cases, including each unsafe-target subtest).

- [ ] **Step 6: Run the full htmx suite**

Run: `just test ./htmx/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add htmx/response.go htmx/response_test.go
git commit -m "feat(htmx): add open-redirect-safe RedirectBack/RedirectBackParam"
```

---

## Task 4: `LocationTarget`

Path + target shortcut over `LocationWith`. Returns no error because a path+target payload (two strings) cannot fail to marshal.

**Files:**
- Modify: `htmx/response.go` (add function after `LocationWith`)
- Test: `htmx/response_test.go`

**Interfaces:**
- Consumes: existing `LocationWith(w, r, path string, opts LocationOptions, status ...int) error` and `LocationOptions`.
- Produces: `LocationTarget(w http.ResponseWriter, r *http.Request, path, target string, status ...int)`.

- [ ] **Step 1: Write the failing tests**

Add to `htmx/response_test.go`:

```go
func TestLocationTargetHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.LocationTarget(rec, htmxRequest(), "/dashboard", "#main")

	assert.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &got))
	assert.Equal(t, "/dashboard", got.Path)
	assert.Equal(t, "#main", got.Target)
}

func TestLocationTargetNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.LocationTarget(rec, r, "/dashboard", "#main")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Location"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./htmx/ -run '^TestLocationTarget' -v`
Expected: FAIL — build error `undefined: htmx.LocationTarget`.

- [ ] **Step 3: Implement `LocationTarget`**

In `htmx/response.go`, add after `LocationWith`:

```go
// LocationTarget is Location that swaps the new content into a target element. For HTMX
// requests it sets HX-Location to {"path":path,"target":target} and writes 200; for
// non-HTMX requests it falls back to http.Redirect to path (default 303 See Other).
// Unlike LocationWith it returns no error — a path+target payload (two strings) cannot
// fail to marshal. Terminal — call it last.
func LocationTarget(w http.ResponseWriter, r *http.Request, path, target string, status ...int) {
	_ = LocationWith(w, r, path, LocationOptions{Target: target}, status...)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./htmx/ -run '^TestLocationTarget' -v`
Expected: PASS (both cases).

- [ ] **Step 5: Commit**

```bash
git add htmx/response.go htmx/response_test.go
git commit -m "feat(htmx): add LocationTarget path+target shortcut"
```

---

## Task 5: `render.Components` + `ErrNoComponents`

A generic, HTMX-agnostic helper that renders several components into one transactional response body. This is the only code OOB needs; OOB-ness lives in the caller's markup.

**Files:**
- Modify: `render/errors.go` (add `ErrNoComponents`)
- Create: `render/components.go`
- Test: `render/components_test.go` (reuses `fakeComponent` and `testCtxKey` from `render/templ_test.go`, same `render_test` package)

**Interfaces:**
- Consumes: existing `Component` interface, `getBuf`/`putBuf`, `setContentType`, `contentTypeHTML`, `ErrNilComponent` (all in package `render`).
- Produces: `var ErrNoComponents error`; `func Components(ctx context.Context, w http.ResponseWriter, status int, components ...Component) error`.

- [ ] **Step 1: Write the failing tests**

Create `render/components_test.go`:

```go
package render_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestComponents_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusCreated,
		&fakeComponent{out: "<p>a</p>"},
		&fakeComponent{out: "<p>b</p>"},
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "<p>a</p><p>b</p>", rec.Body.String()) // order preserved, concatenated
}

func TestComponents_NoComponents(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusOK)
	require.ErrorIs(t, err, render.ErrNoComponents)
	assert.Empty(t, rec.Body.String())
}

func TestComponents_NilComponentWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusOK,
		&fakeComponent{out: "<p>a</p>"}, nil)
	require.ErrorIs(t, err, render.ErrNilComponent)
	assert.Empty(t, rec.Body.String())
}

func TestComponents_RenderErrorWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Components(context.Background(), rec, http.StatusAccepted,
		&fakeComponent{out: "<p>a</p>"},
		&fakeComponent{err: errors.New("boom")},
	)
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // recorder default; nothing committed
	assert.Empty(t, rec.Body.String())
}

func TestComponents_PreservesPresetContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/xml")
	err := render.Components(context.Background(), rec, http.StatusOK,
		&fakeComponent{out: "<x/>"})
	require.NoError(t, err)
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./render/ -run '^TestComponents' -v`
Expected: FAIL — build errors `undefined: render.Components` and `undefined: render.ErrNoComponents`.

- [ ] **Step 3: Add the `ErrNoComponents` sentinel**

In `render/errors.go`, add below `ErrNilComponent`:

```go
// ErrNoComponents is returned by Components when no components are provided.
var ErrNoComponents = errors.New("render: no components")
```

- [ ] **Step 4: Implement `Components`**

Create `render/components.go`:

```go
package render

import (
	"context"
	"fmt"
	"net/http"
)

// Components renders each component into one pooled buffer in order, then writes the
// result with the given status. It is transactional: any Render error returns with
// nothing written to w. It returns ErrNilComponent if any component is nil, and
// ErrNoComponents if none are given (both before writing anything). The Content-Type
// defaults to "text/html; charset=utf-8" unless the caller has already set one.
//
// Use it for multi-fragment responses — for example an HTMX main fragment plus
// out-of-band fragments whose markup carries hx-swap-oob.
func Components(ctx context.Context, w http.ResponseWriter, status int, components ...Component) error {
	if len(components) == 0 {
		return ErrNoComponents
	}
	for _, c := range components {
		if c == nil {
			return ErrNilComponent
		}
	}
	buf := getBuf()
	defer putBuf(buf)
	for _, c := range components {
		if err := c.Render(ctx, buf); err != nil {
			return fmt.Errorf("render: render component: %w", err)
		}
	}
	setContentType(w, contentTypeHTML)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write components: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./render/ -run '^TestComponents' -v`
Expected: PASS (all five cases).

- [ ] **Step 6: Run the full render suite**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add render/errors.go render/components.go render/components_test.go
git commit -m "feat(render): add Components multi-fragment writer"
```

---

## Task 6: Documentation + runnable examples

Document the new redirect helpers and the OOB pattern, and add runnable godoc examples. OOB needs no feature code — it is templ composition rendered by `render.Templ`/`render.Components`, plus the `Reswap(SwapNone)` recipe for OOB-only responses.

**Files:**
- Modify: `htmx/doc.go` (redirect-helpers note + OOB section)
- Modify: `render/doc.go` (mention `Components`)
- Modify: `htmx/example_test.go` (add `ExampleRedirectBack`, `ExampleRedirectExternal`)

**Interfaces:**
- Consumes: `htmx.RedirectBack`, `htmx.RedirectExternal` (Tasks 2–3); `render.Components` (Task 5).
- Produces: documentation and two runnable examples (no new exported symbols).

- [ ] **Step 1: Write the failing example tests**

Add to `htmx/example_test.go` (imports `fmt`, `net/http`, `net/http/httptest`, and `htmx` are already present):

```go
func ExampleRedirectBack() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login?redirect=/dashboard", nil) // non-HTMX request

	htmx.RedirectBack(rec, r, "/home") // honors the safe local ?redirect=, else /home

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Location"))
	// Output:
	// 303
	// /dashboard
}

func ExampleRedirectExternal() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/checkout", nil)
	r.Header.Set("HX-Request", "true") // HTMX request

	htmx.RedirectExternal(rec, r, "https://pay.example.com/session/42")

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("HX-Redirect"))
	// Output:
	// 200
	// https://pay.example.com/session/42
}
```

- [ ] **Step 2: Run the examples to verify they fail**

Run: `go test ./htmx/ -run '^Example(RedirectBack|RedirectExternal)$' -v`
Expected: FAIL — build error `undefined: htmx.RedirectBack` only if Tasks 2–3 were skipped; otherwise the examples run. (If Tasks 2–3 are done, this step instead confirms the examples PASS — proceed.)

- [ ] **Step 3: Add the OOB + redirect documentation to `htmx/doc.go`**

In `htmx/doc.go`, insert the following two paragraphs immediately before the final line `// htmx depends only on the standard library; it does not import render.`:

```go
// Redirect, Location, and their variants are HTMX-aware. RedirectBack returns the user
// to a safe local path from a query parameter (defaulting to "redirect"), falling back
// when that path is absent or not same-origin — it never honors an external URL, so it
// is safe against open redirects. RedirectExternal is the explicit opposite: a
// deliberate full-page redirect to an off-site URL. Use RedirectExternal (not the
// Location family) for external destinations — HX-Location is an AJAX swap and only
// works same-origin.
//
// Out-of-band (OOB) swaps are a markup concern, not a header one: give a fragment's root
// element hx-swap-oob="true" and render it alongside the main fragment in the same
// response body. Compose them in templ and render with the render package — render.Templ
// for a static pair, or render.Components for a dynamic set. For an OOB-only response
// (update other regions, swap nothing into the target), pair it with Reswap(w,
// SwapNone) and render just the OOB fragment(s).
```

- [ ] **Step 4: Mention `Components` in `render/doc.go`**

In `render/doc.go`, in the opening sentence list of helpers, change `Templ (a-h/templ components, via a` to include `Components`:

Replace:

```go
// Package render provides small, stateless helpers for writing HTTP responses from a
// handler: JSON/JSONStream, HTML (html/template), Templ (a-h/templ components, via a
// structural interface — no dependency), Text, Blob, CSV, Stream, Attachment,
// File/FileFS, Redirect, and NoContent.
```

with:

```go
// Package render provides small, stateless helpers for writing HTTP responses from a
// handler: JSON/JSONStream, HTML (html/template), Templ (a-h/templ components, via a
// structural interface — no dependency), Components (several components in one body),
// Text, Blob, CSV, Stream, Attachment, File/FileFS, Redirect, and NoContent.
```

- [ ] **Step 5: Run the full suites and the doc examples**

Run: `just test ./htmx/ ./render/`
Expected: PASS, including `ExampleRedirectBack` and `ExampleRedirectExternal`.

- [ ] **Step 6: Run fmt + lint across the repo**

Run: `just fmt` then `just lint`
Expected: no diffs that break the build; `go vet`, `golangci-lint`, `nilaway`, `betteralign`, and `modernize` all clean. (If `just fmt` reorders struct fields via betteralign, re-run `just test ./htmx/ ./render/` to confirm green.)

- [ ] **Step 7: Commit**

```bash
git add htmx/doc.go render/doc.go htmx/example_test.go
git commit -m "docs(htmx): document redirect helpers, OOB pattern, and render.Components"
```

---

## Final verification

- [ ] **Run the full check recipe**

Run: `just check`
Expected: fmt clean, lint clean, all tests pass with race detector and coverage across `./...`.

---

## Self-Review

**1. Spec coverage** — every spec section maps to a task:
- §1 `RedirectBack`/`RedirectBackParam` + `safeLocalPath` → Task 3.
- §1a `RedirectExternal` → Task 2.
- §2 `LocationTarget` → Task 4.
- §3 typed `Swap` (constants, `Reswap`, `LocationOptions.Swap`, `locationPayload.Swap`) → Task 1.
- §4 `render.Components` + `ErrNoComponents` → Task 5.
- §5 OOB documentation (composition + `Reswap(SwapNone)` recipe) → Task 6.
- "Public API delta" symbols all produced across Tasks 1–5; docs in Task 6.
- "Testing" bullets covered: RedirectBack/Param (Task 3), RedirectExternal (Task 2), LocationTarget (Task 4), typed Swap (Task 1), Components order/transactional/nil/empty/content-type (Task 5).
- Non-goals respected: no HTML rendering in `htmx`, no `Config`/`With*` revival, no exported `HeaderHX*`, `render` learns nothing about HTMX (Components has no `HX-*` knowledge).

**2. Placeholder scan** — no TBD/TODO/"add error handling"/"similar to Task N"; every code step shows complete code.

**3. Type consistency** — `Swap` named type and typed constants (Task 1) are used consistently by `Reswap`, `LocationOptions.Swap`, `locationPayload.Swap`; `RedirectBack` delegates to `RedirectBackParam` which delegates to `Redirect` (signatures match: `status ...int`); `RedirectExternal` delegates to `Redirect`; `LocationTarget` delegates to `LocationWith`; `Components` uses the existing `Component`, `getBuf`/`putBuf`, `setContentType`, `contentTypeHTML`, `ErrNilComponent`, and the new `ErrNoComponents`. Function names and parameter orders match the spec's Public API delta verbatim.
