package htmx

import "net/http"

// IsHTMX returns true if the request originated from HTMX.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get(HeaderHXRequest) == "true"
}
