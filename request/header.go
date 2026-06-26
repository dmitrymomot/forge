package request

import (
	"net/http"
	"strings"
)

// Header reads request header key and converts it to T.
func Header[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceHeader, key, r.Header.Get(key), parse[T], def)
}

// HeaderFunc is Header with a caller-supplied parser.
func HeaderFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceHeader, key, r.Header.Get(key), parse, def)
}

// HasHeader reports whether header key is present and non-empty.
func HasHeader(r *http.Request, key string) bool {
	return r.Header.Get(key) != ""
}

// BearerToken returns the token from an Authorization: Bearer <token> header, or
// "" and false if the header is absent or not a Bearer credential.
func BearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(auth[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}
