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
