package health

import (
	"encoding/json"
	"net/http"
	"strings"
)

// LivenessHandler returns an http.HandlerFunc that always responds OK.
// Use for Kubernetes liveness probes to indicate the process is running.
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wantsJSON(r) {
			writeJSON(w, http.StatusOK, &Response{Status: StatusHealthy})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}
}

// ReadinessHandler returns an http.HandlerFunc that runs all provided checks.
// Use for Kubernetes readiness probes to indicate the service can accept traffic.
func ReadinessHandler(checks Checks, opts ...Option) http.HandlerFunc {
	cfg := newConfig(opts...)

	return func(w http.ResponseWriter, r *http.Request) {
		resp := runChecks(r.Context(), checks, cfg)

		status := http.StatusOK
		if resp.Status == StatusUnhealthy {
			status = http.StatusServiceUnavailable
		}

		if wantsJSON(r) {
			writeJSON(w, status, resp)
			return
		}

		w.WriteHeader(status)
		if resp.Status == StatusHealthy {
			_, _ = w.Write([]byte("OK"))
		} else {
			_, _ = w.Write([]byte("Service Unavailable"))
		}
	}
}

// wantsJSON checks if the client wants JSON response.
func wantsJSON(r *http.Request) bool {
	// Check query parameter first (easier for debugging)
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	// Check Accept header
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
