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
