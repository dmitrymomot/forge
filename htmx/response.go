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
