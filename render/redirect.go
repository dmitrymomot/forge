package render

import "net/http"

// Redirect issues an HTTP redirect to url with the given 3xx status code (e.g.
// http.StatusFound or http.StatusSeeOther). It is a thin wrapper over http.Redirect.
func Redirect(w http.ResponseWriter, r *http.Request, status int, url string) {
	http.Redirect(w, r, url, status)
}
