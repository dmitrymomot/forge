# Design: `htmx` — HTMX header helpers (free functions)

- **Date:** 2026-06-26
- **Status:** Draft for review
- **Scope:** A new standalone `htmx` package of stateless free functions that read
  HTMX request headers and write HTMX response directives — `IsRequest`, `IsBoosted`,
  `Target`, `TriggerID`, … on the request side; `Redirect`, `Location`, `Refresh`,
  `PushURL`, `ReplaceURL`, `Reswap`, `Retarget`, `Reselect`, and the `Trigger` family
  on the response side. No constructor, no options object, no global state. It maps the
  [HTMX header contract](https://htmx.org/reference/#headers) and nothing more — HTML
  output stays in `render`. Stdlib only (`net/http`, `encoding/json`, `strings`,
  `fmt`); the rest of the framework is untouched.

## Overview

`htmx` is a thin, opinion-light layer over `*http.Request` / `http.ResponseWriter`
headers. Each function either reads one `HX-*` request header or sets one `HX-*`
response header, so handlers stop hand-repeating `r.Header.Get("HX-Request") ==
"true"` and `w.Header().Set("HX-Retarget", …)`, and get one consistent, tested place
for the fiddly parts (the names-vs-JSON `HX-Trigger` forms, the HTMX-aware redirect
fallback, the `HX-Location` context payload).

It is deliberately **free functions, not a configured client or a request-scoped
wrapper type**. There is nothing to hold and nothing to register — the handler owns
its `w`/`r` and calls a function. This matches the framework's "no magic" stance and
composes with stdlib handlers and `render` directly:

```go
mux.HandleFunc("POST /cart/items", func(w http.ResponseWriter, r *http.Request) {
    item, err := cart.Add(r.Context(), r.FormValue("sku"))
    if err != nil {
        _ = render.Templ(r.Context(), w, http.StatusUnprocessableEntity, views.CartError(err))
        return
    }
    if htmx.IsRequest(r) {
        htmx.Retarget(w, "#cart")
        htmx.Trigger(w, "cart:updated")
        _ = render.Templ(r.Context(), w, http.StatusOK, views.CartFragment(item)) // partial
        return
    }
    _ = render.Templ(r.Context(), w, http.StatusOK, views.CartPage(item))         // full page
})
```

**Why a package and not just `w.Header().Set`?** Four things the raw headers make you
re-solve at every call site: (1) **the HTMX-aware redirect** — `HX-Redirect` /
`HX-Location` are no-ops for a non-HTMX client, so a correct redirect must branch on
the request type and fall back to a real `3xx`; (2) **the two `HX-Trigger` forms** —
bare names (`saved, closed`) vs. a JSON object with event detail
(`{"saved":{"id":42}}`); (3) **the `HX-Location` context payload** — a JSON object
(`path` + `target`/`swap`/`values`/…) that must be assembled and escaped; (4) the
small but real risk of **typos in magic strings** (`"HX-Reswap"`, `"innerHTML"`).
`htmx` solves each once, with stdlib only.

**Relationship to `render` (one-way, and standalone).** The `render` spec
([2026-06-26-render-package-design.md](2026-06-26-render-package-design.md), "Future
fit") anticipated this package and speculated its redirect branch would call
`render.Redirect`. **That is superseded here:** `htmx` depends only on `net/http`
(plus `encoding/json`), calling `http.Redirect` directly for the non-HTMX fallback. It
imports neither `render` nor anything else in the framework, so the two packages stay
independent and a consumer can use either alone. `render` never learns about HTMX;
`htmx` never renders HTML.

## The write model: terminal vs non-terminal

This is the central behavioral contract, and the reason `Redirect`/`Location` differ
from every other function in the package.

- **Non-terminal directive setters** — `Refresh`, `PushURL`, `ReplaceURL`, `Reswap`,
  `Retarget`, `Reselect`, and the whole `Trigger` family. Each only calls
  `w.Header().Set(...)`. It does **not** write a status or body. The caller writes the
  response afterward (normally via `render`), so these must run **before** the body
  write — the same ordering rule any header has. They cannot fail, except the JSON
  `*With` variants (marshal error). These have no meaningful non-HTMX behavior (they
  shape how HTMX swaps/refreshes content), so they are unconditional: a non-HTMX
  client simply ignores the header.

- **Terminal redirect helpers** — `Redirect`, `Location`, `LocationWith`. These
  **branch on the request type** and **commit the response**:
  - **HTMX request** (`HX-Request: true`): set the `HX-Redirect` / `HX-Location`
    header and `WriteHeader(http.StatusOK)`. HTMX reads the header and performs the
    client-side navigation; the `2xx`/body is irrelevant to it.
  - **Non-HTMX request**: fall back to `http.Redirect(w, r, url, status)` with the
    caller-supplied `3xx`. A plain browser, a boosted-off link, a crawler, or a
    JS-disabled client gets a real redirect instead of a dead `200`.

  Because they call `WriteHeader`, they are terminal — use them as the last statement
  in the branch, exactly like `render.Redirect`. The `status` argument is the fallback
  code and mirrors `render.Redirect(w, r, status, url)` for muscle memory; in the HTMX
  branch it is implicitly `200`.

Rule of thumb: **redirects negotiate and commit; every other directive just sets a
header for HTMX to interpret.**

## Goals

- One stateless free function per request header read and per response directive;
  consistent signature shapes.
- A correct, single-call **HTMX-aware redirect** that degrades to a standard `3xx` for
  non-HTMX clients (`Redirect`, `Location`, `LocationWith`).
- Both `HX-Trigger` forms, cleanly split: bare names (cannot fail) vs. JSON detail
  (returns a marshal error) — for all three trigger timings.
- Discoverable enumerable values via exported `Swap*` constants and `PreventHistory`,
  with plain-`string` parameters (stdlib `http.MethodGet` / `time.RFC3339` style).
- Stdlib only; no `Config`, no options object for the package itself; testify in tests
  only.
- Black-box tests (`package htmx_test`) over the exported surface.

## Non-goals

- **HTML rendering.** No templates, no components, no fragment-vs-page selection
  helper. The handler decides and calls `render`. (Header-helpers-only boundary, set
  during design.)
- **A request-scoped wrapper type** (`hx.From(r).IsBoosted()`). Rejected in favor of
  free functions, consistent with `render`.
- **Out-of-band swap assembly, template partials, or `hx-*` attribute generation.**
  Those are view-layer concerns, not the HTTP header contract.
- **Server-Sent Events / the HTMX SSE extension.** A future transport concern, not a
  header helper.
- **A dependency on `render`** (or any non-stdlib package). The non-HTMX redirect
  branch calls `net/http` directly.
- **Caching-correctness automation.** When one URL serves both an HTMX partial and a
  full page, a shared cache can serve the wrong variant; the fix is `Vary:
  HX-Request`. This is documented as a caller responsibility (see "Behavior details"),
  not silently injected.

## Package & module

- Import path: `github.com/dmitrymomot/forge/htmx`, package `htmx`.
- Flat top-level layout alongside `render`/`httpserver`/`hostrouter`/`supervisor`.
- Stdlib only: `encoding/json`, `fmt`, `net/http`, `strings`.
- File layout (split by direction, mirroring `render`'s split-by-concern):

  ```
  htmx/
    doc.go            # package doc + runnable Example
    request.go        # request-header introspection
    response.go       # response directives + exported Swap*/PreventHistory constants
    headers.go        # internal: HX-* header-name constants + shared trigger writers
    errors.go         # error-prefix note (only the *With marshal errors; no sentinels)
    request_test.go response_test.go example_test.go
  ```

  All `*_test.go` files are `package htmx_test` (black-box) — see "Testing". If
  `errors.go` ends up holding no exported sentinel (see "Errors"), it is folded into
  `response.go` rather than left empty.

## Public API

```go
// --- request.go ------------------------------------------------------------

// IsRequest reports whether r was issued by HTMX (HX-Request: true). Use it to
// branch between returning a partial (HTMX) and a full page.
func IsRequest(r *http.Request) bool

// IsBoosted reports whether r came from an hx-boost'd element (HX-Boosted: true).
func IsBoosted(r *http.Request) bool

// IsHistoryRestore reports whether r is an HTMX history-restoration request after a
// local history-cache miss (HX-History-Restore-Request: true).
func IsHistoryRestore(r *http.Request) bool

// CurrentURL returns the browser's current URL (HX-Current-URL), or "" if absent.
func CurrentURL(r *http.Request) string

// Prompt returns the user's response to an hx-prompt (HX-Prompt), or "" if absent.
func Prompt(r *http.Request) string

// Target returns the id of the target element (HX-Target), or "" if absent.
func Target(r *http.Request) string

// TriggerID returns the id of the element that triggered the request (HX-Trigger),
// or "" if absent. Named TriggerID, not Trigger, to avoid confusion with the
// response-side Trigger (which fires client-side events) — same header name,
// opposite direction.
func TriggerID(r *http.Request) string

// TriggerName returns the name of the triggering element (HX-Trigger-Name), or "".
func TriggerName(r *http.Request) string

// --- response.go: redirects (terminal, HTMX-aware) -------------------------

// Redirect performs a client-side redirect. For HTMX requests it sets HX-Redirect
// (a full-page client navigation) and writes 200; for non-HTMX requests it falls
// back to http.Redirect with status (a 3xx, e.g. http.StatusSeeOther). It commits
// the response — call it last in the branch. Distinct from render.Redirect, which is
// always a plain 3xx.
func Redirect(w http.ResponseWriter, r *http.Request, status int, url string)

// Location performs a client-side redirect without a full page reload. For HTMX
// requests it sets HX-Location to url (an AJAX swap of the new content); for non-HTMX
// requests it falls back to http.Redirect with status. Terminal.
func Location(w http.ResponseWriter, r *http.Request, status int, url string)

// LocationWith is Location with an AJAX context object (target, swap, values, …).
// For HTMX requests it sets HX-Location to the JSON form {"path":path, …opts} and
// writes 200; a marshal failure returns a wrapped error with nothing written. For
// non-HTMX requests it falls back to http.Redirect(w, r, status, path) and the
// context is dropped (a plain browser cannot honor target/swap). Terminal.
func LocationWith(w http.ResponseWriter, r *http.Request, status int, path string, opts LocationOptions) error

// LocationOptions is the optional AJAX context for LocationWith. Zero-value fields
// are omitted from the emitted JSON. It is a plain options struct (cf. http.Cookie),
// not functional options — functional options suit long-lived constructors, while a
// one-shot JSON payload is idiomatically a struct.
type LocationOptions struct {
    Source  string            // CSS selector of the element issuing the request
    Event   string            // event that triggered the request
    Handler string            // callback that handles the response
    Target  string            // CSS selector to swap into
    Swap    string            // how to swap (a Swap* value)
    Select  string            // CSS selector of the response subset to swap
    Values  map[string]any    // values to submit with the request
    Headers map[string]string // headers to submit with the request
}

// --- response.go: history & refresh ----------------------------------------

// PushURL pushes url into the browser history (HX-Push-Url). Pass PreventHistory to
// suppress HTMX's default history update.
func PushURL(w http.ResponseWriter, url string)

// ReplaceURL replaces the current browser URL (HX-Replace-Url). Pass PreventHistory
// to suppress the default replacement.
func ReplaceURL(w http.ResponseWriter, url string)

// Refresh tells the client to do a full page refresh (HX-Refresh: true).
func Refresh(w http.ResponseWriter)

// --- response.go: swap controls --------------------------------------------

// Reswap overrides how the response is swapped in (HX-Reswap). swap is a Swap* value,
// optionally with modifiers (e.g. "innerHTML swap:1s").
func Reswap(w http.ResponseWriter, swap string)

// Retarget changes the element the response is swapped into (HX-Retarget); selector
// is a CSS selector.
func Retarget(w http.ResponseWriter, selector string)

// Reselect chooses which part of the response is swapped in (HX-Reselect),
// overriding hx-select on the triggering element; selector is a CSS selector.
func Reselect(w http.ResponseWriter, selector string)

// --- response.go: client-side events ---------------------------------------

// Trigger fires client-side events by name (HX-Trigger: "a, b"). No names is a no-op
// (no header written). For events carrying detail, use TriggerWith.
func Trigger(w http.ResponseWriter, names ...string)

// TriggerWith fires client-side events with JSON detail
// (HX-Trigger: {"name": detail, …}). An empty map is a no-op; a marshal failure
// returns a wrapped error with no header written.
func TriggerWith(w http.ResponseWriter, events map[string]any) error

// TriggerAfterSettle / TriggerAfterSettleWith fire events after the settle step
// (HX-Trigger-After-Settle). Same name/detail split as Trigger.
func TriggerAfterSettle(w http.ResponseWriter, names ...string)
func TriggerAfterSettleWith(w http.ResponseWriter, events map[string]any) error

// TriggerAfterSwap / TriggerAfterSwapWith fire events after the swap step
// (HX-Trigger-After-Swap). Same name/detail split as Trigger.
func TriggerAfterSwap(w http.ResponseWriter, names ...string)
func TriggerAfterSwapWith(w http.ResponseWriter, events map[string]any) error

// --- response.go: exported constants ---------------------------------------

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

// PreventHistory, passed to PushURL/ReplaceURL, suppresses HTMX's history update
// (the literal "false" HTMX expects).
const PreventHistory = "false"
```

## Internal helpers (`headers.go`)

```go
const (
    // request
    hdrRequest        = "HX-Request"
    hdrBoosted        = "HX-Boosted"
    hdrHistoryRestore = "HX-History-Restore-Request"
    hdrCurrentURL     = "HX-Current-URL"
    hdrPrompt         = "HX-Prompt"
    hdrTarget         = "HX-Target"
    hdrTrigger        = "HX-Trigger" // request: triggering element id; response: events
    hdrTriggerName    = "HX-Trigger-Name"

    // response
    hdrRedirect          = "HX-Redirect"
    hdrLocation          = "HX-Location"
    hdrPushURL           = "HX-Push-Url"
    hdrReplaceURL        = "HX-Replace-Url"
    hdrRefresh           = "HX-Refresh"
    hdrReswap            = "HX-Reswap"
    hdrRetarget          = "HX-Retarget"
    hdrReselect          = "HX-Reselect"
    hdrTriggerAfterSettle = "HX-Trigger-After-Settle"
    hdrTriggerAfterSwap   = "HX-Trigger-After-Swap"
)

// setEventNames backs Trigger/AfterSettle/AfterSwap: bare comma-joined names.
func setEventNames(w http.ResponseWriter, header string, names []string) {
    if len(names) == 0 {
        return
    }
    w.Header().Set(header, strings.Join(names, ", "))
}

// setEventDetail backs the *With variants: JSON {"name": detail, …}.
func setEventDetail(w http.ResponseWriter, header string, events map[string]any) error {
    if len(events) == 0 {
        return nil
    }
    b, err := json.Marshal(events)
    if err != nil {
        return fmt.Errorf("htmx: marshal %s: %w", header, err) // nothing written yet
    }
    w.Header().Set(header, string(b))
    return nil
}
```

The three trigger timings are one-liners over these two helpers, e.g.
`Trigger(w, names...)` → `setEventNames(w, hdrTrigger, names)` and
`TriggerAfterSwapWith(w, events)` → `setEventDetail(w, hdrTriggerAfterSwap, events)`.

The redirect trio shares the branch shape:

```go
func Redirect(w http.ResponseWriter, r *http.Request, status int, url string) {
    if IsRequest(r) {
        w.Header().Set(hdrRedirect, url)
        w.WriteHeader(http.StatusOK)
        return
    }
    http.Redirect(w, r, url, status)
}
```

`LocationWith` marshals **before** touching `w`, so a marshal error leaves the
response untouched (the caller can still send a clean error):

```go
func LocationWith(w http.ResponseWriter, r *http.Request, status int, path string, opts LocationOptions) error {
    if !IsRequest(r) {
        http.Redirect(w, r, path, status) // fallback drops the context
        return nil
    }
    b, err := json.Marshal(locationPayload{Path: path, /* …opts via omitempty… */})
    if err != nil {
        return fmt.Errorf("htmx: marshal HX-Location: %w", err) // nothing written yet
    }
    w.Header().Set(hdrLocation, string(b))
    w.WriteHeader(http.StatusOK)
    return nil
}
```

`locationPayload` is an internal struct with `json:"…,omitempty"` tags mapping
`LocationOptions` plus the required `path`, so only set fields appear in the JSON.

## Errors (`errors.go`)

Only the JSON `*With` functions (`TriggerWith`, `TriggerAfterSettleWith`,
`TriggerAfterSwapWith`, `LocationWith`) can fail, and only via `json.Marshal`. They
return a single-line, `htmx:`-prefixed wrapped error (`htmx: marshal HX-Trigger: %w`)
— matching the framework's single-line-error rule (no multi-line blobs; verbose
context belongs on the structured logger). There are **no exported sentinels**: a
marshal failure is a programmer error (an unserializable detail value), distinguished
by inspection/logging, not by `errors.Is`. If a sentinel proves useful for a
black-box test later it can be added; until then `errors.go` is folded into
`response.go`. Every other function returns nothing, because setting a header cannot
fail.

## Behavior details & edge cases

- **Boolean headers.** `IsRequest`/`IsBoosted`/`IsHistoryRestore` test the value `==
  "true"` (what HTMX sends), not mere presence — robust to an empty or stray value.
- **Missing headers.** Every string getter returns `""` for an absent header
  (`http.Header.Get` semantics); booleans return `false`.
- **`Trigger` with no names / `TriggerWith` with an empty map** write nothing (no
  header) — a clean no-op, so a conditional event list needs no guard at the call
  site.
- **One `HX-Trigger` header per timing.** All setters use `Set` (not `Add`), so a
  second call overwrites the first. To emit both bare names and detail in one timing,
  use the `*With` variant and give detail-less events a `nil` value
  (`{"closed": nil}`); don't call `Trigger` and `TriggerWith` for the same timing.
- **Redirect terminality.** `Redirect`/`Location`/`LocationWith` call `WriteHeader`;
  calling them and then writing a body (or another status) triggers net/http's
  "superfluous WriteHeader". Documented as caller error — they are the last statement
  in the branch.
- **`LocationWith` marshal failure is transactional** (marshal precedes any write);
  the name/detail trigger `*With` variants are likewise transactional (the only write
  is the header `Set`, which happens after a successful marshal).
- **Non-HTMX fallback uses `http.Redirect`,** inheriting its behavior: it writes a
  small HTML body and `Location` header, and (for `StatusMovedPermanently` etc.)
  whatever method semantics the chosen `3xx` implies. Pick `http.StatusSeeOther` for
  the common POST→GET case.
- **Caching / `Vary`.** A handler that returns different bodies for HTMX vs. non-HTMX
  at the same URL should add `w.Header().Add("Vary", "HX-Request")` so shared caches
  don't cross-serve variants. The package documents this in `doc.go` but does not set
  it automatically (it can't know whether the two branches actually differ).
- **`status` validity.** Callers pass a valid `3xx`; `http.Redirect`/`net/http` own
  validation. The package does not second-guess it.

## Usage

```go
// Request introspection
if htmx.IsRequest(r) { /* return a partial */ }
if htmx.IsBoosted(r) { /* hx-boost navigation */ }
city := r.FormValue(htmx.Prompt(r)) // user's hx-prompt answer, etc.

// HTMX-aware redirect (partial-or-full handled for free)
htmx.Redirect(w, r, http.StatusSeeOther, "/dashboard")           // HX-Redirect or 303
htmx.Location(w, r, http.StatusSeeOther, "/dashboard")           // HX-Location or 303
_ = htmx.LocationWith(w, r, http.StatusSeeOther, "/dashboard",   // JSON context or 303
    htmx.LocationOptions{Target: "#main", Swap: htmx.SwapInnerHTML})

// History & refresh
htmx.PushURL(w, "/items/42")
htmx.ReplaceURL(w, htmx.PreventHistory)
htmx.Refresh(w)

// Swap controls (set before rendering the body)
htmx.Reswap(w, htmx.SwapOuterHTML)
htmx.Retarget(w, "#cart")
htmx.Reselect(w, "#cart-rows")

// Client-side events
htmx.Trigger(w, "cart:updated", "toast")
_ = htmx.TriggerWith(w, map[string]any{"toast": map[string]any{"level": "info", "msg": "Saved"}})
htmx.TriggerAfterSwap(w, "focusSearch")

// Then render the fragment (htmx owns headers; render owns the body)
_ = render.Templ(r.Context(), w, http.StatusOK, views.CartFragment(item))
```

## Testing

**Black-box** — all tests live in `package htmx_test` and import the package, so they
exercise only the exported surface (no reaching into `setEventNames`,
`locationPayload`, or the header-name constants). Driven by `httptest.NewRequest` and
`httptest.NewRecorder`; testify only. Internal helpers are covered through the public
functions that call them.

- **Request getters (`request_test.go`):** table-driven — header present with the
  expected value, header absent (zero value), and for the booleans a non-`"true"`
  value (e.g. `"false"`, `""`) → `false`. Confirms `TriggerID` reads `HX-Trigger`
  and `TriggerName` reads `HX-Trigger-Name`.
- **Redirect trio (`response_test.go`):**
  - HTMX request (`HX-Request: true`): `Redirect` sets `HX-Redirect: <url>`, status
    `200`, and no `Location`; `Location`/`LocationWith` set `HX-Location` (the latter
    a JSON object containing `path` + the set options, asserted by unmarshaling), with
    `200`.
  - Non-HTMX request: all three fall back — `Location: <url>` header, the `3xx` status
    passed in, and **no** `HX-*` header. `LocationWith` redirects to `path` and emits
    no JSON (context dropped).
  - `LocationWith` marshal failure on an HTMX request (an `opts.Values` holding an
    unmarshalable value, e.g. `chan int`) → returns a wrapped error **and** the
    recorder shows nothing written (no `HX-Location`, default status, empty body).
- **History/refresh/swap setters:** assert the exact header value for `PushURL`,
  `ReplaceURL` (incl. `PreventHistory` → `"false"`), `Refresh` (`"true"`), `Reswap`,
  `Retarget`, `Reselect`.
- **Trigger family:** `Trigger(w, "a", "b")` → `HX-Trigger: a, b`; `Trigger(w)` →
  header absent; `TriggerWith` → exact JSON (asserted by unmarshaling), empty map →
  header absent, marshal error → wrapped error and no header; the `AfterSettle` /
  `AfterSwap` variants write their respective headers (one representative case each).
- **`example_test.go`:** a runnable `Example` showing the fragment-vs-full-page
  handler (the Overview snippet), for godoc.

## Future fit

- **`Vary: HX-Request` helper.** If consumers repeatedly add it, a one-line
  `htmx.Vary(w)` (`w.Header().Add("Vary", "HX-Request")`) is a natural peer — deferred
  until there's demonstrated repetition, since the package can't know when the two
  branches differ.
- **SSE / event-stream** support (the HTMX SSE extension) would be a separate
  transport concern, layered with `render`'s future streaming helpers, not a header
  function here.
- **Typed `LocationOptions.Swap`** could become a named type shared with `Reswap` if
  stronger grouping is wanted later; kept as a plain `string` now for consistency with
  the `Swap*` constants and stdlib style.

## Deferred

- HTML rendering, a wrapper type, attribute generation, SSE, an automatic `Vary`
  header, and any non-stdlib dependency — all per "Non-goals", revisited only on
  demonstrated need.
