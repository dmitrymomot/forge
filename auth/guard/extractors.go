package guard

import (
	"net/http"
	"net/url"
	"strings"
)

// Extractor pulls a raw credential from a request. ok=false means "not
// present" — the middleware tries the next extractor in the chain; it never
// means "present but bad" (that judgment belongs to the Verifier).
type Extractor func(r *http.Request) (credential string, ok bool)

// BearerHeader extracts the token from an "Authorization: Bearer <token>"
// header (scheme match is case-insensitive). A non-Bearer Authorization
// header reads as no credential, so e.g. a Basic header on a
// bearer-guarded route falls through to the next extractor.
func BearerHeader() Extractor {
	return func(r *http.Request) (string, bool) {
		scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
		if !found || !strings.EqualFold(scheme, "Bearer") {
			return "", false
		}
		token = strings.TrimSpace(token)
		return token, token != ""
	}
}

// Header extracts the named header's value verbatim (e.g. "X-API-Key").
//
// name is canonicalized once here instead of on every request: r.Header.Get
// re-canonicalizes its argument on each call, which allocates whenever name
// isn't already in canonical form (e.g. "X-API-Key" vs "X-Api-Key";
// benchmark in the PR).
func Header(name string) Extractor {
	name = http.CanonicalHeaderKey(name)
	return func(r *http.Request) (string, bool) {
		v := r.Header.Get(name)
		return v, v != ""
	}
}

// Cookie extracts the named cookie's value. For signed or encrypted cookies
// write a closure over web/cookie's Codec instead — any func with this
// signature is an Extractor.
func Cookie(name string) Extractor {
	return func(r *http.Request) (string, bool) {
		c, err := r.Cookie(name)
		if err != nil || c.Value == "" {
			return "", false
		}
		return c.Value, true
	}
}

// Query extracts the named query parameter.
//
// Credentials in query strings leak into access logs, browser history, and
// Referer headers. Prefer BearerHeader or Cookie; reserve Query for signed,
// short-lived links.
//
// It scans RawQuery directly instead of calling r.URL.Query(), which would
// allocate the full url.Values map per request (benchmark in the PR).
func Query(name string) Extractor {
	return func(r *http.Request) (string, bool) {
		q := r.URL.RawQuery
		for q != "" {
			var pair string
			pair, q, _ = strings.Cut(q, "&")
			k, v, _ := strings.Cut(pair, "=")
			if strings.ContainsAny(k, "%+") {
				dec, err := url.QueryUnescape(k)
				if err != nil {
					continue
				}
				k = dec
			}
			if k != name {
				continue
			}
			if strings.ContainsAny(v, "%+") {
				dec, err := url.QueryUnescape(v)
				if err != nil {
					return "", false
				}
				v = dec
			}
			return v, v != ""
		}
		return "", false
	}
}
