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

// HasHeader reports whether header key is present (even with an empty value),
// consistent with HasQuery/HasForm.
func HasHeader(r *http.Request, key string) bool {
	return len(r.Header.Values(key)) > 0
}

// BearerToken returns the token from an Authorization: Bearer <token> header, or
// "" and false if the header is absent or not a Bearer credential. The scheme is
// matched case-insensitively and surrounding whitespace is trimmed from the token.
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
