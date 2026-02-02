package htmx

import (
	"net/http"
)

// Redirect performs a redirect for both HTMX and regular requests.
func Redirect(w http.ResponseWriter, r *http.Request, url string) {
	RedirectWithStatus(w, r, url, http.StatusFound)
}

// RedirectWithStatus performs a redirect with a custom status code.
func RedirectWithStatus(w http.ResponseWriter, r *http.Request, targetURL string, status int) {
	if IsHTMX(r) {
		w.Header().Set(HeaderHXRedirect, targetURL)
		// HTMX requires 200 status; actual redirect happens client-side via header
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, targetURL, status)
}

// RedirectBack redirects to the URL in the "redirect" query parameter, or fallback if not present.
func RedirectBack(w http.ResponseWriter, r *http.Request, fallback string) {
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" {
		redirectURL = fallback
	}

	Redirect(w, r, redirectURL)
}
