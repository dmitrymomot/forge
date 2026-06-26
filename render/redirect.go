package render

import "net/http"

// Redirect issues an HTTP redirect to url. status must be a 3xx code (e.g.
// http.StatusFound or http.StatusSeeOther); a non-3xx code silently sets the
// Location header and writes an HTML body — no error is returned. It is a thin
// wrapper over http.Redirect.
func Redirect(w http.ResponseWriter, r *http.Request, status int, url string) {
	http.Redirect(w, r, url, status)
}
