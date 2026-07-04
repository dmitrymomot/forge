package request

import "net/http"

// Path reads path wildcard key and converts it to T. The value comes from
// the current mux match (r.PathValue) or, when absent there, from values
// stored with WithPathValues (e.g. by subroute mount prefixes).
func Path[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourcePath, key, pathValue(r, key), parse[T], def)
}

// PathFunc is Path with a caller-supplied parser.
func PathFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourcePath, key, pathValue(r, key), parse, def)
}

// HasPath reports whether the path wildcard key is set and non-empty,
// checking the current mux match and then WithPathValues fallback.
func HasPath(r *http.Request, key string) bool {
	return pathValue(r, key) != ""
}
