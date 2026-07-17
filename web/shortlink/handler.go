package shortlink

import (
	"errors"
	"net/http"
	"strings"
)

// Handler returns the public redirect endpoint. It reads the code from the
// "code" path value when mounted on a pattern ("GET /{code}"), falling back
// to the full decoded path with slashes trimmed, and redirects with the
// configured status (302 by default, never a cacheable 301) and
// Cache-Control: no-store so intermediaries cannot swallow hits. Dead links
// (unknown, expired, or deactivated codes) redirect to the WithFallbackURL
// target when one is configured, or respond 404; a Store or hook failure
// responds 500 — an outage must read as an outage, not as every link being
// gone.
//
//	mux.Handle("GET /{code}", mgr.Handler())
func (m *Manager) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		if code == "" {
			code = strings.Trim(r.URL.Path, "/")
		}
		w.Header().Set("Cache-Control", "no-store")
		l, err := m.Resolve(r.Context(), code)
		switch {
		case err == nil:
			http.Redirect(w, r, l.URL, m.cfg.redirectStatus)
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrLinkExpired), errors.Is(err, ErrLinkDeactivated):
			if m.cfg.fallbackURL != "" {
				http.Redirect(w, r, m.cfg.fallbackURL, m.cfg.redirectStatus)
				return
			}
			http.NotFound(w, r)
		default:
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	})
}
