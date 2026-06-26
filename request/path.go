package request

import "net/http"

// Path reads path wildcard key (r.PathValue) and converts it to T. Requires a
// Go 1.22+ ServeMux pattern that defines key.
func Path[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourcePath, key, r.PathValue(key), parse[T], def)
}

// PathFunc is Path with a caller-supplied parser.
func PathFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourcePath, key, r.PathValue(key), parse, def)
}

// HasPath reports whether the path wildcard key is set and non-empty.
func HasPath(r *http.Request, key string) bool {
	return r.PathValue(key) != ""
}
