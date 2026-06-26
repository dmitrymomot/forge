# htmx Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone `htmx` package of stateless free functions that read HTMX request headers and write HTMX response directives, mapping the HTMX HTTP header contract.

**Architecture:** Free functions over `*http.Request` (readers) and `http.ResponseWriter` (writers), mirroring the `render` package. Most writers only set one `HX-*` header (non-terminal, called before the body is rendered). The redirect trio (`Redirect`, `Location`, `LocationWith`) is the exception: it branches on `IsRequest(r)` — setting `HX-Redirect`/`HX-Location` for HTMX requests, or falling back to `http.Redirect` (a real 3xx) otherwise — and commits the response (terminal). Stdlib only; no dependency on `render`.

**Tech Stack:** Go 1.26 standard library (`net/http`, `encoding/json`, `strings`, `fmt`); `testify` in tests only; `just` task runner.

**Spec:** [docs/superpowers/specs/2026-06-26-htmx-package-design.md](../specs/2026-06-26-htmx-package-design.md)

## Global Constraints

- **Go 1.26**, module `github.com/dmitrymomot/forge`; new package import path `github.com/dmitrymomot/forge/htmx`.
- **Stdlib only** in package code — no external dependencies. `testify` (already a module dep) allowed in tests only.
- **Black-box tests ONLY** — every `*_test.go` file declares `package htmx_test` and imports the package.
- **Flat layout** — files live directly under `htmx/`, no subfolders.
- **No builder pattern** — `LocationOptions` is a plain options struct (zero-value fields omitted), not functional options and not a builder.
- **Single-line errors**, prefixed `htmx:` and `%w`-wrapped (e.g. `htmx: marshal HX-Location: %w`). Only the JSON `*With` functions return errors.
- **Public funcs never return unexported types** — `LocationWith` returns `error`; `locationPayload` stays internal.
- **Use `just` recipes** — `just test ./htmx/` to run; `just check` (fmt + lint + test) before finishing.
- Enumerable header values are plain `string` parameters with exported `Swap*` / `PreventHistory` constants (stdlib `http.MethodGet` style).

---

### Task 1: Request-header introspection

**Files:**
- Create: `htmx/doc.go`
- Create: `htmx/headers.go`
- Create: `htmx/request.go`
- Test: `htmx/request_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func IsRequest(r *http.Request) bool` — used by Task 4's redirect trio.
  - `func IsBoosted(r *http.Request) bool`
  - `func IsHistoryRestore(r *http.Request) bool`
  - `func CurrentURL(r *http.Request) string`
  - `func Prompt(r *http.Request) string`
  - `func Target(r *http.Request) string`
  - `func TriggerID(r *http.Request) string`
  - `func TriggerName(r *http.Request) string`
  - Internal request header-name constants in `headers.go` (`hdrRequest`, `hdrBoosted`, `hdrHistoryRestore`, `hdrCurrentURL`, `hdrPrompt`, `hdrTarget`, `hdrTrigger`, `hdrTriggerName`) — `hdrTrigger` is reused by Task 3.

- [ ] **Step 1: Write the failing test**

Create `htmx/request_test.go`:

```go
package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/htmx"
	"github.com/stretchr/testify/assert"
)

func TestRequestBooleans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		value  string
		fn     func(*http.Request) bool
		want   bool
	}{
		{"IsRequest true", "HX-Request", "true", htmx.IsRequest, true},
		{"IsRequest false value", "HX-Request", "false", htmx.IsRequest, false},
		{"IsRequest absent", "", "", htmx.IsRequest, false},
		{"IsBoosted true", "HX-Boosted", "true", htmx.IsBoosted, true},
		{"IsBoosted absent", "", "", htmx.IsBoosted, false},
		{"IsHistoryRestore true", "HX-History-Restore-Request", "true", htmx.IsHistoryRestore, true},
		{"IsHistoryRestore absent", "", "", htmx.IsHistoryRestore, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			assert.Equal(t, tc.want, tc.fn(r))
		})
	}
}

func TestRequestStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		value  string
		fn     func(*http.Request) string
	}{
		{"CurrentURL", "HX-Current-URL", "https://example.com/x", htmx.CurrentURL},
		{"Prompt", "HX-Prompt", "yes", htmx.Prompt},
		{"Target", "HX-Target", "#main", htmx.Target},
		{"TriggerID", "HX-Trigger", "save-btn", htmx.TriggerID},
		{"TriggerName", "HX-Trigger-Name", "save", htmx.TriggerName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(tc.header, tc.value)
			assert.Equal(t, tc.value, tc.fn(r))

			absent := httptest.NewRequest(http.MethodGet, "/", nil)
			assert.Empty(t, tc.fn(absent))
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./htmx/`
Expected: FAIL — build error (`undefined: htmx.IsRequest`, etc., or "no non-test Go files in .../htmx").

- [ ] **Step 3: Create the package doc, header constants, and getters**

Create `htmx/doc.go`:

```go
// Package htmx provides small, stateless helpers for the HTMX HTTP header
// contract: reading HX-* request headers (IsRequest, IsBoosted, Target,
// TriggerID, ...) and writing HX-* response directives (Redirect, Location,
// Refresh, PushURL, Reswap, Retarget, Reselect, and the Trigger family).
//
// The helpers are free functions — there is no constructor, options object, or
// global state. HTML output is not this package's concern; render the body with
// the render package (or any handler) after setting the htmx headers:
//
//	if htmx.IsRequest(r) {
//		htmx.Retarget(w, "#cart")
//		htmx.Trigger(w, "cart:updated")
//		_ = render.Templ(r.Context(), w, http.StatusOK, views.CartFragment(item))
//		return
//	}
//	_ = render.Templ(r.Context(), w, http.StatusOK, views.CartPage(item))
//
// Most directives only set a response header and must be called before the body
// is written. The redirect helpers (Redirect, Location, LocationWith) are the
// exception: they branch on whether the request came from HTMX — setting the
// HX-Redirect / HX-Location header and a 200 for HTMX requests, or falling back
// to a standard http.Redirect (3xx) for everyone else — and they commit the
// response, so call them last.
//
// When one URL serves both an HTMX partial and a full page, add
// Vary: HX-Request so shared caches do not cross-serve the two variants.
//
// htmx depends only on the standard library; it does not import render.
package htmx
```

Create `htmx/headers.go`:

```go
package htmx

// Request header names. hdrTrigger is reused by the response-side Trigger.
const (
	hdrRequest        = "HX-Request"
	hdrBoosted        = "HX-Boosted"
	hdrHistoryRestore = "HX-History-Restore-Request"
	hdrCurrentURL     = "HX-Current-URL"
	hdrPrompt         = "HX-Prompt"
	hdrTarget         = "HX-Target"
	hdrTrigger        = "HX-Trigger" // request: triggering element id; response: events
	hdrTriggerName    = "HX-Trigger-Name"
)
```

Create `htmx/request.go`:

```go
package htmx

import "net/http"

// IsRequest reports whether r was issued by HTMX (HX-Request: true).
func IsRequest(r *http.Request) bool {
	return r.Header.Get(hdrRequest) == "true"
}

// IsBoosted reports whether r came from an hx-boost'd element (HX-Boosted: true).
func IsBoosted(r *http.Request) bool {
	return r.Header.Get(hdrBoosted) == "true"
}

// IsHistoryRestore reports whether r is an HTMX history-restoration request
// (HX-History-Restore-Request: true).
func IsHistoryRestore(r *http.Request) bool {
	return r.Header.Get(hdrHistoryRestore) == "true"
}

// CurrentURL returns the browser's current URL (HX-Current-URL), or "" if absent.
func CurrentURL(r *http.Request) string {
	return r.Header.Get(hdrCurrentURL)
}

// Prompt returns the user's response to an hx-prompt (HX-Prompt), or "" if absent.
func Prompt(r *http.Request) string {
	return r.Header.Get(hdrPrompt)
}

// Target returns the id of the target element (HX-Target), or "" if absent.
func Target(r *http.Request) string {
	return r.Header.Get(hdrTarget)
}

// TriggerID returns the id of the element that triggered the request
// (HX-Trigger), or "" if absent. Named TriggerID, not Trigger, to avoid
// confusion with the response-side Trigger (which fires client-side events).
func TriggerID(r *http.Request) string {
	return r.Header.Get(hdrTrigger)
}

// TriggerName returns the name of the triggering element (HX-Trigger-Name), or "".
func TriggerName(r *http.Request) string {
	return r.Header.Get(hdrTriggerName)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./htmx/`
Expected: PASS — `ok  github.com/dmitrymomot/forge/htmx`.

- [ ] **Step 5: Commit**

```bash
git add htmx/doc.go htmx/headers.go htmx/request.go htmx/request_test.go
git commit -m "feat(htmx): request-header introspection helpers"
```

---

### Task 2: Response directives — history, refresh, swap

**Files:**
- Create: `htmx/response.go`
- Modify: `htmx/headers.go` (append a response header-name const block)
- Test: `htmx/response_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (uses only `net/http`).
- Produces:
  - `func PushURL(w http.ResponseWriter, url string)`
  - `func ReplaceURL(w http.ResponseWriter, url string)`
  - `func Refresh(w http.ResponseWriter)`
  - `func Reswap(w http.ResponseWriter, swap string)`
  - `func Retarget(w http.ResponseWriter, selector string)`
  - `func Reselect(w http.ResponseWriter, selector string)`
  - Exported constants `SwapInnerHTML`, `SwapOuterHTML`, `SwapTextContent`, `SwapBeforeBegin`, `SwapAfterBegin`, `SwapBeforeEnd`, `SwapAfterEnd`, `SwapDelete`, `SwapNone` (used by Task 4), and `PreventHistory` (= `"false"`).

- [ ] **Step 1: Write the failing test**

Create `htmx/response_test.go`:

```go
package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/htmx"
	"github.com/stretchr/testify/assert"
)

func TestSimpleSetters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		apply  func(http.ResponseWriter)
		header string
		want   string
	}{
		{"PushURL", func(w http.ResponseWriter) { htmx.PushURL(w, "/items/42") }, "HX-Push-Url", "/items/42"},
		{"PushURL prevent", func(w http.ResponseWriter) { htmx.PushURL(w, htmx.PreventHistory) }, "HX-Push-Url", "false"},
		{"ReplaceURL", func(w http.ResponseWriter) { htmx.ReplaceURL(w, "/x") }, "HX-Replace-Url", "/x"},
		{"Refresh", func(w http.ResponseWriter) { htmx.Refresh(w) }, "HX-Refresh", "true"},
		{"Reswap", func(w http.ResponseWriter) { htmx.Reswap(w, htmx.SwapOuterHTML) }, "HX-Reswap", "outerHTML"},
		{"Retarget", func(w http.ResponseWriter) { htmx.Retarget(w, "#cart") }, "HX-Retarget", "#cart"},
		{"Reselect", func(w http.ResponseWriter) { htmx.Reselect(w, "#rows") }, "HX-Reselect", "#rows"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			tc.apply(rec)
			assert.Equal(t, tc.want, rec.Header().Get(tc.header))
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./htmx/`
Expected: FAIL — build error (`undefined: htmx.PushURL`, `htmx.SwapOuterHTML`, etc.).

- [ ] **Step 3: Append response header-name constants, then write the setters**

Append to `htmx/headers.go`:

```go
// Response header names: history, refresh, and swap controls.
const (
	hdrPushURL    = "HX-Push-Url"
	hdrReplaceURL = "HX-Replace-Url"
	hdrRefresh    = "HX-Refresh"
	hdrReswap     = "HX-Reswap"
	hdrRetarget   = "HX-Retarget"
	hdrReselect   = "HX-Reselect"
)
```

Create `htmx/response.go`:

```go
package htmx

import "net/http"

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

// PreventHistory, passed to PushURL or ReplaceURL, suppresses HTMX's history
// update (the literal "false" HTMX expects).
const PreventHistory = "false"

// PushURL pushes url into the browser history (HX-Push-Url). Pass PreventHistory
// to suppress HTMX's default history update.
func PushURL(w http.ResponseWriter, url string) {
	w.Header().Set(hdrPushURL, url)
}

// ReplaceURL replaces the current browser URL (HX-Replace-Url). Pass
// PreventHistory to suppress the default replacement.
func ReplaceURL(w http.ResponseWriter, url string) {
	w.Header().Set(hdrReplaceURL, url)
}

// Refresh tells the client to do a full page refresh (HX-Refresh: true).
func Refresh(w http.ResponseWriter) {
	w.Header().Set(hdrRefresh, "true")
}

// Reswap overrides how the response is swapped in (HX-Reswap). swap is a Swap*
// value, optionally with modifiers (e.g. "innerHTML swap:1s").
func Reswap(w http.ResponseWriter, swap string) {
	w.Header().Set(hdrReswap, swap)
}

// Retarget changes the element the response is swapped into (HX-Retarget);
// selector is a CSS selector.
func Retarget(w http.ResponseWriter, selector string) {
	w.Header().Set(hdrRetarget, selector)
}

// Reselect chooses which part of the response is swapped in (HX-Reselect),
// overriding hx-select on the triggering element; selector is a CSS selector.
func Reselect(w http.ResponseWriter, selector string) {
	w.Header().Set(hdrReselect, selector)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./htmx/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add htmx/headers.go htmx/response.go htmx/response_test.go
git commit -m "feat(htmx): response directives for history, refresh, and swap"
```

---

### Task 3: Client-side event Trigger family

**Files:**
- Modify: `htmx/headers.go` (append trigger header-name constants + shared writers + imports)
- Modify: `htmx/response.go` (append the six Trigger functions)
- Modify: `htmx/response_test.go` (append Trigger tests + imports)

**Interfaces:**
- Consumes: `hdrTrigger` constant (Task 1).
- Produces:
  - `func Trigger(w http.ResponseWriter, names ...string)`
  - `func TriggerWith(w http.ResponseWriter, events map[string]any) error`
  - `func TriggerAfterSettle(w http.ResponseWriter, names ...string)`
  - `func TriggerAfterSettleWith(w http.ResponseWriter, events map[string]any) error`
  - `func TriggerAfterSwap(w http.ResponseWriter, names ...string)`
  - `func TriggerAfterSwapWith(w http.ResponseWriter, events map[string]any) error`
  - Internal `setEventNames` / `setEventDetail` helpers in `headers.go`.

- [ ] **Step 1: Write the failing tests**

Replace the import block at the top of `htmx/response_test.go` with (adds `encoding/json` and `testify/require`):

```go
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/htmx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

Append to `htmx/response_test.go`:

```go
func TestTriggerNames(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Trigger(rec, "cart:updated", "toast")
	assert.Equal(t, "cart:updated, toast", rec.Header().Get("HX-Trigger"))
}

func TestTriggerNoNamesIsNoOp(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Trigger(rec)
	_, ok := rec.Header()["HX-Trigger"]
	assert.False(t, ok)
}

func TestTriggerWith(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.TriggerWith(rec, map[string]any{"toast": map[string]any{"level": "info"}})
	require.NoError(t, err)

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Trigger")), &got))
	assert.Equal(t, "info", got["toast"]["level"])
}

func TestTriggerWithEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	require.NoError(t, htmx.TriggerWith(rec, map[string]any{}))
	_, ok := rec.Header()["HX-Trigger"]
	assert.False(t, ok)
}

func TestTriggerWithMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.TriggerWith(rec, map[string]any{"bad": make(chan int)})
	require.Error(t, err)
	_, ok := rec.Header()["HX-Trigger"]
	assert.False(t, ok) // nothing written on marshal failure
}

func TestTriggerAfterVariants(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.TriggerAfterSettle(rec, "settled")
	htmx.TriggerAfterSwap(rec, "swapped")
	assert.Equal(t, "settled", rec.Header().Get("HX-Trigger-After-Settle"))
	assert.Equal(t, "swapped", rec.Header().Get("HX-Trigger-After-Swap"))

	bad := httptest.NewRecorder()
	require.Error(t, htmx.TriggerAfterSettleWith(bad, map[string]any{"x": make(chan int)}))
	require.NoError(t, htmx.TriggerAfterSwapWith(httptest.NewRecorder(), map[string]any{"y": 1}))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `just test ./htmx/`
Expected: FAIL — build error (`undefined: htmx.Trigger`, `htmx.TriggerWith`, etc.).

- [ ] **Step 3: Add the shared writers and the Trigger functions**

Replace the contents of `htmx/headers.go` so it gains an import block, the trigger header-name constants, and the two shared writers (the existing const blocks are unchanged):

```go
package htmx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Request header names. hdrTrigger is reused by the response-side Trigger.
const (
	hdrRequest        = "HX-Request"
	hdrBoosted        = "HX-Boosted"
	hdrHistoryRestore = "HX-History-Restore-Request"
	hdrCurrentURL     = "HX-Current-URL"
	hdrPrompt         = "HX-Prompt"
	hdrTarget         = "HX-Target"
	hdrTrigger        = "HX-Trigger" // request: triggering element id; response: events
	hdrTriggerName    = "HX-Trigger-Name"
)

// Response header names: history, refresh, and swap controls.
const (
	hdrPushURL    = "HX-Push-Url"
	hdrReplaceURL = "HX-Replace-Url"
	hdrRefresh    = "HX-Refresh"
	hdrReswap     = "HX-Reswap"
	hdrRetarget   = "HX-Retarget"
	hdrReselect   = "HX-Reselect"
)

// Response header names: client-side event triggers.
const (
	hdrTriggerAfterSettle = "HX-Trigger-After-Settle"
	hdrTriggerAfterSwap   = "HX-Trigger-After-Swap"
)

// setEventNames writes a bare comma-joined event-name list (the HX-Trigger
// family's name form). No names is a no-op.
func setEventNames(w http.ResponseWriter, header string, names []string) {
	if len(names) == 0 {
		return
	}
	w.Header().Set(header, strings.Join(names, ", "))
}

// setEventDetail writes the JSON {"name": detail, ...} form. An empty map is a
// no-op; a marshal failure returns a wrapped error with no header written.
func setEventDetail(w http.ResponseWriter, header string, events map[string]any) error {
	if len(events) == 0 {
		return nil
	}
	b, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("htmx: marshal %s: %w", header, err)
	}
	w.Header().Set(header, string(b))
	return nil
}
```

Append to `htmx/response.go`:

```go
// Trigger fires client-side events by name (HX-Trigger: "a, b"). No names is a
// no-op. For events carrying detail, use TriggerWith.
func Trigger(w http.ResponseWriter, names ...string) {
	setEventNames(w, hdrTrigger, names)
}

// TriggerWith fires client-side events with JSON detail
// (HX-Trigger: {"name": detail, ...}). An empty map is a no-op; a marshal
// failure returns a wrapped error with no header written.
func TriggerWith(w http.ResponseWriter, events map[string]any) error {
	return setEventDetail(w, hdrTrigger, events)
}

// TriggerAfterSettle fires events after the settle step (HX-Trigger-After-Settle).
func TriggerAfterSettle(w http.ResponseWriter, names ...string) {
	setEventNames(w, hdrTriggerAfterSettle, names)
}

// TriggerAfterSettleWith fires events with JSON detail after the settle step.
func TriggerAfterSettleWith(w http.ResponseWriter, events map[string]any) error {
	return setEventDetail(w, hdrTriggerAfterSettle, events)
}

// TriggerAfterSwap fires events after the swap step (HX-Trigger-After-Swap).
func TriggerAfterSwap(w http.ResponseWriter, names ...string) {
	setEventNames(w, hdrTriggerAfterSwap, names)
}

// TriggerAfterSwapWith fires events with JSON detail after the swap step.
func TriggerAfterSwapWith(w http.ResponseWriter, events map[string]any) error {
	return setEventDetail(w, hdrTriggerAfterSwap, events)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `just test ./htmx/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add htmx/headers.go htmx/response.go htmx/response_test.go
git commit -m "feat(htmx): client-side event Trigger family"
```

---

### Task 4: HTMX-aware Redirect / Location with non-HTMX fallback

**Files:**
- Modify: `htmx/headers.go` (append redirect header-name constants)
- Modify: `htmx/response.go` (append redirect funcs + `LocationOptions` + `locationPayload` + imports)
- Modify: `htmx/response_test.go` (append redirect tests)

**Interfaces:**
- Consumes: `IsRequest` (Task 1); `SwapInnerHTML` (Task 2, used in tests).
- Produces:
  - `func Redirect(w http.ResponseWriter, r *http.Request, status int, url string)`
  - `func Location(w http.ResponseWriter, r *http.Request, status int, url string)`
  - `func LocationWith(w http.ResponseWriter, r *http.Request, status int, path string, opts LocationOptions) error`
  - `type LocationOptions struct { Source, Event, Handler, Target, Swap, Select string; Values map[string]any; Headers map[string]string }`

- [ ] **Step 1: Write the failing tests**

Append to `htmx/response_test.go` (its import block already has `encoding/json`, `require`, `net/http`, `httptest` from Task 3):

```go
func htmxRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("HX-Request", "true")
	return r
}

func TestRedirectHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Redirect(rec, htmxRequest(), http.StatusSeeOther, "/dashboard")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("HX-Redirect"))
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestRedirectNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.Redirect(rec, r, http.StatusSeeOther, "/dashboard")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
}

func TestLocationHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Location(rec, htmxRequest(), http.StatusSeeOther, "/dashboard")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("HX-Location"))
}

func TestLocationNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.Location(rec, r, http.StatusSeeOther, "/dashboard")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Location"))
}

func TestLocationWithHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.LocationWith(rec, htmxRequest(), http.StatusSeeOther, "/dashboard",
		htmx.LocationOptions{Target: "#main", Swap: htmx.SwapInnerHTML})
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Path   string `json:"path"`
		Target string `json:"target"`
		Swap   string `json:"swap"`
	}
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &got))
	assert.Equal(t, "/dashboard", got.Path)
	assert.Equal(t, "#main", got.Target)
	assert.Equal(t, "innerHTML", got.Swap)
}

func TestLocationWithNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	err := htmx.LocationWith(rec, r, http.StatusSeeOther, "/dashboard",
		htmx.LocationOptions{Target: "#main"})
	require.NoError(t, err)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Location"))
}

func TestLocationWithMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.LocationWith(rec, htmxRequest(), http.StatusSeeOther, "/dashboard",
		htmx.LocationOptions{Values: map[string]any{"bad": make(chan int)}})
	require.Error(t, err)
	assert.Empty(t, rec.Header().Get("HX-Location")) // nothing written
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `just test ./htmx/`
Expected: FAIL — build error (`undefined: htmx.Redirect`, `htmx.LocationOptions`, etc.).

- [ ] **Step 3: Add the redirect constants and functions**

Append to `htmx/headers.go`:

```go
// Response header names: client-side redirects.
const (
	hdrRedirect = "HX-Redirect"
	hdrLocation = "HX-Location"
)
```

Replace the import line in `htmx/response.go` (`import "net/http"`) with:

```go
import (
	"encoding/json"
	"fmt"
	"net/http"
)
```

Append to `htmx/response.go`:

```go
// LocationOptions is the optional AJAX context for LocationWith. Zero-value
// fields are omitted from the emitted JSON. It is a plain options struct
// (cf. http.Cookie), not functional options and not a builder.
type LocationOptions struct {
	Source  string            // CSS selector of the element issuing the request
	Event   string            // event that triggered the request
	Handler string            // callback that handles the response
	Target  string            // CSS selector to swap into
	Swap    string            // how to swap (a Swap* value)
	Select  string            // CSS selector of the response subset to swap
	Values  map[string]any    // values submitted with the request
	Headers map[string]string // headers submitted with the request
}

// locationPayload is the JSON shape HX-Location expects: the required path plus
// the set LocationOptions fields.
type locationPayload struct {
	Path    string            `json:"path"`
	Source  string            `json:"source,omitempty"`
	Event   string            `json:"event,omitempty"`
	Handler string            `json:"handler,omitempty"`
	Target  string            `json:"target,omitempty"`
	Swap    string            `json:"swap,omitempty"`
	Select  string            `json:"select,omitempty"`
	Values  map[string]any    `json:"values,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Redirect performs a client-side redirect. For HTMX requests it sets
// HX-Redirect (a full-page client navigation) and writes 200; otherwise it falls
// back to http.Redirect with status (a 3xx, e.g. http.StatusSeeOther). It commits
// the response — call it last.
func Redirect(w http.ResponseWriter, r *http.Request, status int, url string) {
	if IsRequest(r) {
		w.Header().Set(hdrRedirect, url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, status)
}

// Location performs a client-side redirect without a full page reload. For HTMX
// requests it sets HX-Location to url and writes 200; otherwise it falls back to
// http.Redirect with status. Terminal.
func Location(w http.ResponseWriter, r *http.Request, status int, url string) {
	if IsRequest(r) {
		w.Header().Set(hdrLocation, url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, status)
}

// LocationWith is Location with an AJAX context object (target, swap, values, ...).
// For HTMX requests it sets HX-Location to the JSON form {"path":path, ...opts}
// and writes 200; a marshal failure returns a wrapped error with nothing written.
// For non-HTMX requests it falls back to http.Redirect(w, r, status, path),
// dropping the context. Terminal.
func LocationWith(w http.ResponseWriter, r *http.Request, status int, path string, opts LocationOptions) error {
	if !IsRequest(r) {
		http.Redirect(w, r, path, status)
		return nil
	}
	b, err := json.Marshal(locationPayload{
		Path:    path,
		Source:  opts.Source,
		Event:   opts.Event,
		Handler: opts.Handler,
		Target:  opts.Target,
		Swap:    opts.Swap,
		Select:  opts.Select,
		Values:  opts.Values,
		Headers: opts.Headers,
	})
	if err != nil {
		return fmt.Errorf("htmx: marshal HX-Location: %w", err)
	}
	w.Header().Set(hdrLocation, string(b))
	w.WriteHeader(http.StatusOK)
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `just test ./htmx/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add htmx/headers.go htmx/response.go htmx/response_test.go
git commit -m "feat(htmx): HTMX-aware Redirect/Location with non-HTMX fallback"
```

---

### Task 5: Runnable examples and final verification

**Files:**
- Create: `htmx/example_test.go`

**Interfaces:**
- Consumes: `IsRequest`, `Retarget`, `Trigger` (Tasks 1–3), `Redirect` (Task 4).
- Produces: nothing (godoc examples only).

- [ ] **Step 1: Write the example tests**

Create `htmx/example_test.go`:

```go
package htmx_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/htmx"
)

func ExampleIsRequest() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cart", nil)
	r.Header.Set("HX-Request", "true")

	if htmx.IsRequest(r) {
		htmx.Retarget(rec, "#cart")
		htmx.Trigger(rec, "cart:updated")
		// then render the partial, e.g. render.Templ(r.Context(), rec, 200, fragment)
	}

	fmt.Println(htmx.IsRequest(r))
	fmt.Println(rec.Header().Get("HX-Retarget"))
	fmt.Println(rec.Header().Get("HX-Trigger"))
	// Output:
	// true
	// #cart
	// cart:updated
}

func ExampleRedirect() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login", nil) // non-HTMX request

	htmx.Redirect(rec, r, http.StatusSeeOther, "/dashboard")

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Location"))
	// Output:
	// 303
	// /dashboard
}
```

- [ ] **Step 2: Run the examples to verify they pass**

Run: `just test ./htmx/`
Expected: PASS (Go runs `Example*` functions and checks their `// Output:` blocks).

- [ ] **Step 3: Run the full check (fmt + lint + test)**

Run: `just check`
Expected: `go fmt`/`goimports`/`betteralign` make no changes; `go vet`, `golangci-lint`, `nilaway`, `betteralign`, `modernize` report nothing; all tests pass. If `betteralign -apply` (run by `just fmt`) reorders any struct field, re-run `just test ./htmx/` and include the change in the commit below.

- [ ] **Step 4: Commit**

```bash
git add htmx/example_test.go
git commit -m "docs(htmx): runnable examples for request, directives, and redirect"
```

---

## Self-Review

**1. Spec coverage** — every spec section maps to a task:

- Request introspection (8 getters) → Task 1.
- Redirect trio with HTMX-aware fallback + terminal semantics → Task 4.
- History/refresh (`PushURL`, `ReplaceURL`, `Refresh`, `PreventHistory`) → Task 2.
- Swap controls (`Reswap`, `Retarget`, `Reselect`) + `Swap*` constants → Task 2.
- Trigger family (names + `*With` detail, three timings) → Task 3.
- `LocationOptions` / `locationPayload` (plain struct, `omitempty`) → Task 4.
- Internal `setEventNames` / `setEventDetail`, header-name constants → Tasks 1–4 (introduced when first used, to satisfy the `unused` linter).
- Errors: single-line `htmx:`-prefixed, only on `*With` marshal failure, no exported sentinels → Tasks 3 & 4 (folded into `response.go`/`headers.go`; no `errors.go` needed).
- `doc.go` package overview + `Vary: HX-Request` note → Task 1; runnable Examples → Task 5.
- Testing: black-box `package htmx_test`, `httptest`-driven, testify; covers absent headers, no-op cases, marshal errors, both redirect branches → Tasks 1–5.

**2. Placeholder scan** — no TBD/TODO/"add error handling"/"similar to Task N"; every code step shows complete code. ✔

**3. Type consistency** — signatures are identical across the Interfaces blocks and the implementation steps: `IsRequest(*http.Request) bool`; `Trigger(http.ResponseWriter, ...string)`; `TriggerWith(http.ResponseWriter, map[string]any) error`; `Redirect/Location(http.ResponseWriter, *http.Request, int, string)`; `LocationWith(http.ResponseWriter, *http.Request, int, string, LocationOptions) error`. `LocationOptions` field set and order match between `response.go` and the `locationPayload` mapping. ✔
