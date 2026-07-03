package request

import "net/http"

// Cookie reads cookie key's value and converts it to T. A missing cookie is treated
// as absent (zero value / default).
func Cookie[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceCookie, key, cookieValue(r, key), parse[T], def)
}

// CookieFunc is Cookie with a caller-supplied parser.
func CookieFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceCookie, key, cookieValue(r, key), parse, def)
}

// HasCookie reports whether cookie key is present.
func HasCookie(r *http.Request, key string) bool {
	_, err := r.Cookie(key)
	return err == nil
}

func cookieValue(r *http.Request, key string) string {
	c, err := r.Cookie(key)
	if err != nil {
		return ""
	}
	return c.Value
}
