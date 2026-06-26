package htmx

import (
	"encoding/json"
	"fmt"
	"net/http"
)

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
	Swap    string            // how to swap (a Swap* value)
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
	Swap    string            `json:"swap,omitempty"`
	Select  string            `json:"select,omitempty"`
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
// For non-HTMX requests it falls back to http.Redirect(w, r, path, status),
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
