package htmx

import (
	"encoding/json"
	"net/http"
)

// LocationOptions represents the configuration for HX-Location header.
type LocationOptions struct {
	Path    string            `json:"path"`
	Source  string            `json:"source,omitempty"`
	Event   string            `json:"event,omitempty"`
	Handler string            `json:"handler,omitempty"`
	Target  string            `json:"target,omitempty"`
	Swap    string            `json:"swap,omitempty"`
	Values  map[string]string `json:"values,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Select  string            `json:"select,omitempty"`
}

// Location performs a client-side navigation with URL update and history entry.
func Location(w http.ResponseWriter, r *http.Request, path string) {
	if IsHTMX(r) {
		w.Header().Set(HeaderHXLocation, path)
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, path, http.StatusFound)
}

// LocationTarget performs a client-side navigation that updates a specific element.
func LocationTarget(w http.ResponseWriter, r *http.Request, path, target string) {
	if IsHTMX(r) {
		opts := LocationOptions{
			Path:   path,
			Target: target,
		}
		LocationWithOptions(w, r, opts)
		return
	}

	http.Redirect(w, r, path, http.StatusFound)
}

// LocationWithOptions performs a client-side navigation with full HTMX location options.
func LocationWithOptions(w http.ResponseWriter, r *http.Request, opts LocationOptions) {
	if IsHTMX(r) {
		jsonData, err := json.Marshal(opts)
		if err != nil {
			// Fallback to path-only if options serialization fails
			w.Header().Set(HeaderHXLocation, opts.Path)
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set(HeaderHXLocation, string(jsonData))
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, opts.Path, http.StatusFound)
}
