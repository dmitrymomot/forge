package assets

import (
	"bytes"
	"net/http"
	"path"
	"strings"
)

// DefaultSPAWhen is the fallback predicate installed by WithSPA: fall back to the
// index for GET/HEAD requests that look like app navigations — no file extension,
// or an explicit Accept: text/html. A request for a concrete asset (extension +
// non-HTML Accept, e.g. a <script>) returns 404 so missing-asset bugs stay
// visible.
func DefaultSPAWhen(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return path.Ext(r.URL.Path) == "" || strings.Contains(r.Header.Get("Accept"), "text/html")
}

// serveSPA serves the configured index with no-cache (never immutable — it is the
// entry point that references the fingerprinted assets and must stay fresh).
func (a *Assets) serveSPA(w http.ResponseWriter, r *http.Request) {
	data, err := readFileFS(a.fsys, a.spaIndex)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", a.revalidateCC)
	if ct := contentType(a.spaIndex); ct != "" {
		h.Set("Content-Type", ct)
	}
	http.ServeContent(w, r, a.spaIndex, statTime(a.fsys, a.spaIndex), bytes.NewReader(data))
}
