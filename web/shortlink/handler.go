package shortlink

import (
	"net/http"
	"strings"
)

// Handler returns the public redirect endpoint. It reads the code from the
// "code" path value when mounted on a pattern ("GET /{code}"), falling back
// to the full escaped path with slashes trimmed, and redirects with the
// configured status (302 by default, never a cacheable 301) and
// Cache-Control: no-store so intermediaries cannot swallow hits. Failed
// resolves redirect to the WithFallbackURL target when one is configured,
// or respond 404.
//
//	mux.Handle("GET /{code}", mgr.Handler())
func (m *Manager) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		if code == "" {
			code = strings.Trim(r.URL.EscapedPath(), "/")
		}
		w.Header().Set("Cache-Control", "no-store")
		l, err := m.Resolve(r.Context(), code)
		if err != nil {
			if m.cfg.fallbackURL != "" {
				http.Redirect(w, r, m.cfg.fallbackURL, m.cfg.redirectStatus)
				return
			}
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, l.URL, m.cfg.redirectStatus)
	})
}
