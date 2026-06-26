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

// Response header names: client-side redirects.
const (
	hdrRedirect = "HX-Redirect"
	hdrLocation = "HX-Location"
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
