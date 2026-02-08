package ratelimit

import (
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/pkg/clientip"
	"github.com/dmitrymomot/forge/pkg/fingerprint"
)

// KeyFunc extracts a rate-limit key from an HTTP request.
type KeyFunc func(r *http.Request) string

// KeyByIP extracts the client IP address as the rate-limit key.
// Uses pkg/clientip which supports CDN headers (CF-Connecting-IP, X-Forwarded-For, etc.).
func KeyByIP(r *http.Request) string {
	return clientip.GetIP(r)
}

// KeyByFingerprint extracts the device fingerprint as the rate-limit key.
// Uses pkg/fingerprint.Cookie which excludes IP for stability across networks.
func KeyByFingerprint(r *http.Request) string {
	return fingerprint.Cookie(r)
}

// KeyByPath extracts the request URL path as the rate-limit key.
func KeyByPath(r *http.Request) string {
	return r.URL.Path
}

// KeyByHeader returns a KeyFunc that extracts the named header value.
func KeyByHeader(name string) KeyFunc {
	return func(r *http.Request) string {
		return r.Header.Get(name)
	}
}

// KeyComposite combines multiple key extractors into a single key
// by joining their non-empty results with ":".
func KeyComposite(funcs ...KeyFunc) KeyFunc {
	return func(r *http.Request) string {
		parts := make([]string, 0, len(funcs))
		for _, fn := range funcs {
			if v := fn(r); v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, ":")
	}
}
