package request

import "net/http"

// Query reads URL query parameter key and converts it to T. Absent/empty yields
// def[0] (or the zero value) with a nil error; a present unparseable value returns
// a *Error with Kind Malformed.
func Query[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceQuery, key, r.URL.Query().Get(key), parse[T], def)
}

// QueryFunc is Query with a caller-supplied parser instead of the built-in engine.
func QueryFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceQuery, key, r.URL.Query().Get(key), parse, def)
}

// QuerySlice reads every repeated value of key (?id=1&id=2) and parses each into T.
func QuerySlice[T any](r *http.Request, key string, def ...[]T) ([]T, error) {
	return resolveSlice(SourceQuery, key, r.URL.Query()[key], parse[T], def)
}

// QuerySliceFunc is QuerySlice with a caller-supplied element parser.
func QuerySliceFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	return resolveSlice(SourceQuery, key, r.URL.Query()[key], parse, def)
}

// QuerySplit reads a single delimited value (?filter=a,b,c), splitting on sep.
func QuerySplit[T any](r *http.Request, key, sep string, def ...[]T) ([]T, error) {
	return resolveSplit(SourceQuery, key, r.URL.Query().Get(key), sep, parse[T], def)
}

// QuerySplitFunc is QuerySplit with a caller-supplied element parser.
func QuerySplitFunc[T any](r *http.Request, key, sep string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	return resolveSplit(SourceQuery, key, r.URL.Query().Get(key), sep, parse, def)
}

// HasQuery reports whether key is present in the URL query (even with an empty value).
func HasQuery(r *http.Request, key string) bool {
	return r.URL.Query().Has(key)
}
