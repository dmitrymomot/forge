package htmx

import (
	"net/http"
	"net/url"
	"strings"
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
//
// The destination from the untrusted "redirect" query parameter is validated to
// prevent open-redirect attacks: only same-origin, path-relative destinations are
// allowed. Any target carrying a scheme/host (e.g. "https://evil.com") or a
// protocol-relative form (e.g. "//evil.com") is rejected and replaced with the
// fallback. The fallback itself is used as-is and is the caller's responsibility.
func RedirectBack(w http.ResponseWriter, r *http.Request, fallback string) {
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" || !isSafeRelativePath(redirectURL) {
		redirectURL = fallback
	}

	Redirect(w, r, redirectURL)
}

// isSafeRelativePath reports whether target is a safe same-origin, path-relative
// destination suitable for a redirect derived from untrusted input.
//
// A target is rejected when it could navigate to a different origin:
//   - it does not begin with "/" (not rooted; relative or scheme-bearing like "http://...")
//   - it begins with "//" or "/\" (protocol-relative, e.g. "//evil.com")
//   - it contains a control character (CR/LF and friends) used for header injection
//
// The same checks are applied to the URL-decoded form of the target so that
// percent-encoded separators ("/%2f/evil.com", "/%5c/evil.com") cannot smuggle a
// protocol-relative destination past the literal checks once a browser decodes
// the path.
func isSafeRelativePath(target string) bool {
	if !isSafeRelativePathLiteral(target) {
		return false
	}

	// A malformed escape sequence is rejected outright rather than passed
	// through unchecked.
	dec, err := url.PathUnescape(target)
	if err != nil {
		return false
	}

	return isSafeRelativePathLiteral(dec)
}

// isSafeRelativePathLiteral runs the same-origin safety checks against a single
// (already-decoded or raw) form of the target.
func isSafeRelativePathLiteral(target string) bool {
	// Must be rooted at the application's own path space.
	if !strings.HasPrefix(target, "/") {
		return false
	}

	// Reject protocol-relative URLs ("//host" and the "/\host" browser quirk),
	// which navigate cross-origin despite starting with a slash.
	if len(target) > 1 && (target[1] == '/' || target[1] == '\\') {
		return false
	}

	// Reject control characters (including CR/LF) that could enable header
	// injection or be normalized by browsers into a different destination.
	for _, c := range target {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}

	return true
}
