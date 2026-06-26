# Design: extend `htmx` — re-add dropped conveniences

- **Date:** 2026-06-26
- **Status:** Draft for review
- **Builds on:** [2026-06-26-htmx-package-design.md](2026-06-26-htmx-package-design.md)
  (the headers-only, stdlib-only, free-function `htmx` package).
- **Scope:** Re-add the genuinely useful conveniences that the v1 `pkg/htmx` package
  had and the v2 rewrite dropped, *without* reopening the boundaries the rewrite drew.
  Additions to `htmx` (`RedirectBack`/`RedirectBackParam`, `RedirectExternal`,
  `LocationTarget`, a typed `Swap`), one generic helper in `render` (`Components`), and
  documentation for out-of-band (OOB) swaps. `htmx` stays headers-only and stdlib-only;
  `render` stays HTMX-agnostic.

## Motivation

A comparison of v1 `pkg/htmx` against the v2 `htmx` rewrite showed the rewrite is a
strict functional superset on the request side (v1 had only `IsHTMX`; v2 adds
`IsBoosted`, `IsHistoryRestore`, `CurrentURL`, `Prompt`, `Target`, `TriggerID`,
`TriggerName`) and an improvement on most of the response side (variadic fallback
`status`, 303 default instead of 302, marshal errors surfaced instead of swallowed,
`LocationOptions.Values` widened to `map[string]any`). But four conveniences were
dropped. This design restores the three that fit the rewrite's philosophy and resolves
the fourth (OOB) the way the architecture now wants it.

## What v1 had that v2 dropped, and the decision for each

| v1 symbol | Decision |
|---|---|
| `LocationTarget(w,r,path,target)` | **Re-add** (terminal, no error — see below). |
| `RedirectBack(w,r,fallback)` | **Re-add, hardened**: open-redirect-safe + configurable param name. |
| `SwapStrategy` + swap constants | **Re-add** as `type Swap string`; v2 already kept the constants (and added `SwapTextContent`). |
| `WithOOB` / `OOBComponents` / `Renderable` | **Replace** with a generic `render.Components` + documented templ composition. No OOB renderer in `htmx`. |
| `Config`/`NewConfig`/`ApplyHeaders` + 9 `With*` options | **Leave dead.** Each capability is already a v2 free function; the aggregator only existed to feed the removed `internal/context.go` render pipeline. |
| 18 exported `HeaderHX*` name constants | **Leave unexported.** Helpers cover all normal use; encapsulation is intentional. Middleware needing a raw name uses the string literal. |

After these changes, every *functional* capability of v1 `pkg/htmx` is available in v2;
the only intentionally-unrestored items are the obsolete `Config` aggregator and the
encapsulated header-name constants.

## 1. `RedirectBack` / `RedirectBackParam` (`response.go`)

HTMX-aware redirect to a caller-supplied return path, defending against open redirects.

```go
// RedirectBack redirects to the local path in the "redirect" query parameter, or to
// fallback when that parameter is absent, empty, or not a safe local path. It is
// HTMX-aware (delegates to Redirect): HX-Redirect + 200 for HTMX requests, a real 3xx
// otherwise. The optional status sets the non-HTMX fallback code (only the first value
// is used), defaulting to http.StatusSeeOther (303). Terminal — call it last.
func RedirectBack(w http.ResponseWriter, r *http.Request, fallback string, status ...int)

// RedirectBackParam is RedirectBack reading a caller-named query parameter instead of
// the default "redirect".
func RedirectBackParam(w http.ResponseWriter, r *http.Request, param, fallback string, status ...int)
```

**Open-redirect protection (the hardening).** v1 redirected to the raw `?redirect=`
value, so `?redirect=https://evil.com` sent users off-site. The candidate target is now
honored only if it is a safe *local* path; otherwise `fallback` is used:

```go
// safeLocalPath reports whether s is a relative, same-origin path safe to redirect to:
// it must begin with a single "/" (not "//" scheme-relative, not "/\" which some
// browsers treat as scheme-relative) and parse to a URL with no Scheme and no Host.
func safeLocalPath(s string) bool {
    if s == "" || s[0] != '/' {
        return false // "", "https://evil", "evil.com", relative non-rooted
    }
    if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
        return false // protocol-relative
    }
    u, err := url.Parse(s)
    return err == nil && u.Scheme == "" && u.Host == ""
}
```

- `RedirectBack` delegates to `RedirectBackParam(w, r, defaultRedirectParam, fallback, status...)`
  where `defaultRedirectParam = "redirect"` (internal const). No global mutable state.
- Both delegate the actual write to `Redirect`, inheriting the HTMX-vs-3xx branch and the
  303 default.
- New stdlib import: `net/url` (still stdlib-only).

## 1a. `RedirectExternal` (`response.go`)

The deliberate counterpart to `RedirectBack`: an explicit off-site redirect for the
cases where leaving the app is the intent (OAuth provider, payment gateway, an external
docs URL). New in v2 (not a v1 symbol) — it closes a real footgun rather than a v1 gap.

```go
// RedirectExternal performs a full-page client redirect to an external (off-site) URL.
// It is the explicit counterpart to RedirectBack's local-only safety: it does NOT
// validate url, so reserve it for developer-controlled destinations, never raw user
// input. For HTMX requests it uses HX-Redirect (a full window.location navigation, which
// works cross-origin); for non-HTMX requests it falls back to http.Redirect with the
// optional status (default 303). Terminal.
//
// Use this, not Location/LocationTarget/LocationWith, for external URLs: those use
// HX-Location, an AJAX swap that is same-origin only and fails on a cross-origin target.
func RedirectExternal(w http.ResponseWriter, r *http.Request, url string, status ...int) {
    Redirect(w, r, url, status...)
}
```

**Why a wrapper over `Redirect`.** Mechanically, `Redirect` already drives the
`HX-Redirect` / full-navigation path that external URLs require, so `RedirectExternal`
delegates to it — zero duplicated logic. Its value is intent and documentation: it reads
as the opposite of `RedirectBack`, and its godoc is where the "external ⇒ HX-Redirect,
never HX-Location" rule is stated. `Redirect` remains the general HTMX-aware redirect
(in-app or external); `RedirectExternal` is the self-documenting form for off-site.

## 2. `LocationTarget` (`response.go`)

Path + target shortcut over `LocationWith`.

```go
// LocationTarget is Location that swaps the new content into a target element. For HTMX
// requests it sets HX-Location to {"path":path,"target":target} and writes 200; for
// non-HTMX requests it falls back to http.Redirect to path (default 303). Unlike
// LocationWith it returns no error — a path+target payload (two strings) cannot fail to
// marshal. Terminal.
func LocationTarget(w http.ResponseWriter, r *http.Request, path, target string, status ...int) {
    _ = LocationWith(w, r, path, LocationOptions{Target: target}, status...)
}
```

The discarded error is provably `nil` (marshaling a struct of two strings never fails),
so the no-error signature is honest, not a swallow. Documented as such.

## 3. Typed `Swap` (`response.go`)

```go
// Swap is an hx-swap style (the HX-Reswap value and LocationOptions.Swap). Use a Swap*
// constant; a modifier reads naturally as SwapInnerHTML + " swap:1s" (an untyped string
// constant added to a Swap stays a Swap, so no conversion is needed).
type Swap string

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

func Reswap(w http.ResponseWriter, swap Swap) // was: swap string; body: Set(hdrReswap, string(swap))
```

- `LocationOptions.Swap` changes `string` → `Swap`.
- `locationPayload.Swap` changes to `Swap` with `omitempty`; it marshals to its
  underlying string, so the emitted JSON is unchanged.
- `PreventHistory` stays a plain `string` const (it is a push/replace value, not a swap).
- **Compatibility:** breaking relative to current v2 code, but a repo-wide search found
  no consumer of the swap API outside `htmx` itself; only `htmx`'s own tests/examples
  need the retype.

## 4. `render.Components` (`render/components.go`)

A generic, HTMX-agnostic multi-fragment writer. This is the only piece OOB needs, and it
earns its place independent of HTMX (any multi-fragment response can use it).

```go
// Components renders each component into one pooled buffer in order, then writes the
// result with the given status. It is transactional: any Render error returns with
// nothing written to w. It returns ErrNilComponent if any component is nil, and
// ErrNoComponents if none are given (both before writing anything). The Content-Type
// defaults to "text/html; charset=utf-8" unless the caller has already set one.
//
// Use it for multi-fragment responses — for example an HTMX main fragment plus
// out-of-band fragments whose markup carries hx-swap-oob.
func Components(ctx context.Context, w http.ResponseWriter, status int, components ...Component) error
```

- Mirrors `Templ`: same `Component` interface, same pooled buffer (`getBuf`/`putBuf`),
  same transactional contract, same content-type handling.
- **Contains zero HTMX knowledge** — it never inspects or sets an `HX-*` header and never
  mentions `hx-swap-oob`. This is what keeps `render` HTMX-agnostic, consistent with the
  decision to keep the HTMX-aware redirect *out* of `render`. Adding a generic capability
  to `render` is not the same as teaching `render` the HTMX contract.
- New sentinel `ErrNoComponents` in `render/errors.go`, alongside the existing
  `ErrNilComponent`; single-line, `render:`-prefixed message style.
- Edge cases: empty variadic → `ErrNoComponents`; any `nil` element → `ErrNilComponent`;
  both checked before any write so the response is untouched on error.

## 5. Out-of-band (OOB) swaps — documentation, not feature code

OOB is a *markup* pattern: an element carrying `hx-swap-oob="true"` (or
`hx-swap-oob="<swap>:<selector>"`) on its own root is swapped into its matching target,
in addition to the main response. The v1 `htmx` package never actually rendered OOB; it
only *carried* a `[]Renderable` for the removed `internal/context.go` pipeline. In v2,
rendering is `render`'s job and OOB falls out of normal composition.

**Static set — existing API, no new code:**

```go
// views/cart.templ
templ CartResponse(cart Cart) {
    @CartRows(cart)        // main → swapped into hx-target
    @CartBadge(cart.Count) // OOB → its own markup carries hx-swap-oob="true"
}

// handler
_ = render.Templ(r.Context(), w, http.StatusOK, views.CartResponse(cart))
```

**Dynamic set — `render.Components`:**

```go
_ = render.Components(r.Context(), w, http.StatusOK,
    views.CartRows(cart),        // main
    views.CartBadge(cart.Count), // OOB (markup carries hx-swap-oob)
)
```

**OOB-only response (no main swap) — paired htmx header helper:**

```go
htmx.Reswap(w, htmx.SwapNone) // tell HTMX to ignore the "main" content
_ = render.Components(r.Context(), w, http.StatusOK, views.CartBadge(cart.Count))
```

Documentation lands in:
- `htmx/doc.go`: an OOB section covering the composition pattern and the `Reswap(SwapNone)`
  OOB-only recipe, and stating that OOB markup/body is rendered via `render`.
- `render/doc.go`: document `Components` (multi-fragment responses, OOB as the motivating
  example).
- `htmx/example_test.go`: a runnable `ExampleRedirectBack`; an OOB example may live as a
  doc snippet (templ markup is not exercisable in a stdlib-only example).

## Public API delta

Net new exported surface:

- `htmx`: `RedirectBack`, `RedirectBackParam`, `RedirectExternal`, `LocationTarget`,
  `Swap` (type) — and the `Swap*` constants change from untyped `string` to typed `Swap`;
  `Reswap` and `LocationOptions.Swap` retype to `Swap`.
- `render`: `Components`, `ErrNoComponents`.

Unchanged: all existing request getters, the redirect trio, the trigger family, the
history/refresh/swap setters, `PreventHistory`. No symbols removed.

## Testing

Black-box (`package htmx_test`, `package render_test`), `httptest` + testify, exercising
only the exported surface — matching the existing suites.

**`htmx` (`response_test.go`):**
- `RedirectBack` / `RedirectBackParam`:
  - HTMX request → `HX-Redirect: <target>`, status 200, no `Location`.
  - Non-HTMX request → `Location: <target>`, the fallback `3xx` (default 303, and an
    overridden code), no `HX-*`.
  - Safe local target (`/dashboard`) honored; unsafe/malformed targets
    (`https://evil.com`, `//evil.com`, `/\evil.com`, `""`, param absent) fall back to
    `fallback`.
  - `RedirectBackParam` reads the custom param name; `RedirectBack` reads `"redirect"`.
- `RedirectExternal`:
  - HTMX request → `HX-Redirect: <external url>`, status 200, no `Location` (full-page
    navigation, not an AJAX swap).
  - Non-HTMX request → `Location: <external url>`, fallback `3xx` (default 303, override).
  - An external URL (`https://example.com/x`) is passed through unchanged (no validation).
- `LocationTarget`:
  - HTMX request → `HX-Location` is JSON `{"path":...,"target":...}` (asserted by
    unmarshaling), status 200.
  - Non-HTMX request → `Location: <path>`, fallback `3xx`, no `HX-Location`.
- Typed `Swap`: `Reswap(w, SwapOuterHTML)` sets `HX-Reswap: outerHTML`; a modifier
  (`SwapInnerHTML + " swap:1s"`) compiles and emits the combined value;
  `LocationOptions{Swap: SwapInnerHTML}` round-trips through `LocationWith`.

**`render` (`components_test.go`):**
- Multiple components render concatenated, in order, with one status write and default
  `text/html`.
- Transactional: a component whose `Render` errors → wrapped error and nothing written
  (recorder empty, default status).
- `nil` element → `ErrNilComponent`, nothing written.
- No components → `ErrNoComponents`, nothing written.
- Pre-set `Content-Type` is preserved.

## Non-goals (unchanged from the base design)

- No HTML rendering in `htmx`; no `Renderable`/`Component` interface in `htmx`; no
  `htmx.OOB` renderer.
- No revival of `Config`/`NewConfig`/`ApplyHeaders`/`With*`.
- No exported `HeaderHX*` constants.
- No SSE, no automatic `Vary: HX-Request`, no non-stdlib dependency in `htmx`.
- `render` gains only the generic `Components`; it learns nothing about HTMX.
