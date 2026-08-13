package htmx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Header names are written in Go's canonical MIME form (Hx-, not HX-), so
// Header.Get and Header.Set take their no-allocation fast path instead of
// canonicalizing on every call. The bytes on the wire are identical either way,
// and HTTP header names are case-insensitive.
//
// Request header names. hdrTrigger is reused by the response-side Trigger.
const (
	hdrRequest        = "Hx-Request"
	hdrBoosted        = "Hx-Boosted"
	hdrHistoryRestore = "Hx-History-Restore-Request"
	hdrCurrentURL     = "Hx-Current-URL"
	hdrPrompt         = "Hx-Prompt"
	hdrTarget         = "Hx-Target"
	hdrTrigger        = "Hx-Trigger" // request: triggering element id; response: events
	hdrTriggerName    = "Hx-Trigger-Name"
)

// Response header names: history, refresh, and swap controls.
const (
	hdrPushURL    = "Hx-Push-Url"
	hdrReplaceURL = "Hx-Replace-Url"
	hdrRefresh    = "Hx-Refresh"
	hdrReswap     = "Hx-Reswap"
	hdrRetarget   = "Hx-Retarget"
	hdrReselect   = "Hx-Reselect"
)

// Response header names: client-side event triggers.
const (
	hdrTriggerAfterSettle = "Hx-Trigger-After-Settle"
	hdrTriggerAfterSwap   = "Hx-Trigger-After-Swap"
)

// Response header names: client-side redirects.
const (
	hdrRedirect = "Hx-Redirect"
	hdrLocation = "Hx-Location"
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
