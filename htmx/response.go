package htmx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

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
func Reswap(w http.ResponseWriter, swap Swap) {
	w.Header().Set(hdrReswap, string(swap))
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

// Trigger fires client-side events by name (HX-Trigger: "a, b"). No names is a
// no-op. Each Trigger function sets a single header, so calling any of them more
// than once for the same header overwrites the previous value (last write wins) —
// pass every event name in one call. For events carrying detail, use TriggerWith.
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
// An empty map is a no-op; a marshal failure returns a wrapped error with no
// header written.
func TriggerAfterSettleWith(w http.ResponseWriter, events map[string]any) error {
	return setEventDetail(w, hdrTriggerAfterSettle, events)
}

// TriggerAfterSwap fires events after the swap step (HX-Trigger-After-Swap).
func TriggerAfterSwap(w http.ResponseWriter, names ...string) {
	setEventNames(w, hdrTriggerAfterSwap, names)
}

// TriggerAfterSwapWith fires events with JSON detail after the swap step.
// An empty map is a no-op; a marshal failure returns a wrapped error with no
// header written.
func TriggerAfterSwapWith(w http.ResponseWriter, events map[string]any) error {
	return setEventDetail(w, hdrTriggerAfterSwap, events)
}

// LocationOptions is the optional AJAX context for LocationWith. Zero-value
// fields are omitted from the emitted JSON. It is a plain options struct
// (cf. http.Cookie), not functional options and not a builder.
type LocationOptions struct {
	Values  map[string]any    // values submitted with the request
	Headers map[string]string // headers submitted with the request
	Source  string            // CSS selector of the element issuing the request
	Event   string            // event that triggered the request
	Handler string            // callback that handles the response
	Target  string            // CSS selector to swap into
	Swap    Swap              // how to swap (a Swap* value)
	Select  string            // CSS selector of the response subset to swap
}

// locationPayload is the JSON shape HX-Location expects: the required path plus
// the set LocationOptions fields.
type locationPayload struct {
	Values  map[string]any    `json:"values,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Path    string            `json:"path"`
	Source  string            `json:"source,omitempty"`
	Event   string            `json:"event,omitempty"`
	Handler string            `json:"handler,omitempty"`
	Target  string            `json:"target,omitempty"`
	Swap    Swap              `json:"swap,omitempty"`
	Select  string            `json:"select,omitempty"`
}

// fallbackStatus returns the non-HTMX redirect status: the first value supplied
// to the redirect helpers, or http.StatusSeeOther (303) when none is given.
func fallbackStatus(status []int) int {
	if len(status) > 0 {
		return status[0]
	}
	return http.StatusSeeOther
}

// Redirect performs a client-side redirect. For HTMX requests it sets
// HX-Redirect (a full-page client navigation) and writes 200. For non-HTMX
// requests it falls back to http.Redirect; the optional status sets that fallback
// code (only the first value is used), defaulting to http.StatusSeeOther (303).
// status never applies to HTMX requests, which always get 200. It commits the
// response — call it last.
func Redirect(w http.ResponseWriter, r *http.Request, url string, status ...int) {
	if IsRequest(r) {
		w.Header().Set(hdrRedirect, url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, fallbackStatus(status))
}

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

// Location performs a client-side redirect without a full page reload. For HTMX
// requests it sets HX-Location to url and writes 200. For non-HTMX requests it
// falls back to http.Redirect; the optional status sets that fallback code (only
// the first value is used), defaulting to http.StatusSeeOther (303). Terminal.
func Location(w http.ResponseWriter, r *http.Request, url string, status ...int) {
	if IsRequest(r) {
		w.Header().Set(hdrLocation, url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, fallbackStatus(status))
}

// LocationWith is Location with an AJAX context object (target, swap, values, ...).
// For HTMX requests it sets HX-Location to the JSON form {"path":path, ...opts}
// and writes 200; a marshal failure returns a wrapped error with nothing written.
// For non-HTMX requests it falls back to http.Redirect — the optional status sets
// that fallback code (only the first value is used), defaulting to
// http.StatusSeeOther (303) — dropping the context. Terminal.
func LocationWith(w http.ResponseWriter, r *http.Request, path string, opts LocationOptions, status ...int) error {
	if !IsRequest(r) {
		http.Redirect(w, r, path, fallbackStatus(status))
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

// LocationTarget is Location that swaps the new content into a target element. For HTMX
// requests it sets HX-Location to {"path":path,"target":target} and writes 200; for
// non-HTMX requests it falls back to http.Redirect to path (default 303 See Other).
// Unlike LocationWith it returns no error — a path+target payload (two strings) cannot
// fail to marshal. Terminal — call it last.
func LocationTarget(w http.ResponseWriter, r *http.Request, path, target string, status ...int) {
	_ = LocationWith(w, r, path, LocationOptions{Target: target}, status...)
}
