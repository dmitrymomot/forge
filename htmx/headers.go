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

// Response header names: history, refresh, and swap controls.
const (
	hdrPushURL    = "HX-Push-Url"
	hdrReplaceURL = "HX-Replace-Url"
	hdrRefresh    = "HX-Refresh"
	hdrReswap     = "HX-Reswap"
	hdrRetarget   = "HX-Retarget"
	hdrReselect   = "HX-Reselect"
)
