package request

import "net/http"

// FormValue reads body form field key (POST/PUT/PATCH) and converts it to T. It
// reads only the request body, never the URL query.
func FormValue[T any](r *http.Request, key string, def ...T) (T, error) {
	return resolve(SourceForm, key, r.PostFormValue(key), parse[T], def)
}

// FormValueFunc is FormValue with a caller-supplied parser.
func FormValueFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...T) (T, error) {
	return resolve(SourceForm, key, r.PostFormValue(key), parse, def)
}

// FormSlice reads every repeated body value of key and parses each into T.
func FormSlice[T any](r *http.Request, key string, def ...[]T) ([]T, error) {
	_ = r.ParseForm() // best-effort: a broken/absent body reads as no values (like net/http)
	return resolveSlice(SourceForm, key, r.PostForm[key], parse[T], def)
}

// FormSliceFunc is FormSlice with a caller-supplied element parser.
func FormSliceFunc[T any](r *http.Request, key string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	_ = r.ParseForm() // best-effort: a broken/absent body reads as no values (like net/http)
	return resolveSlice(SourceForm, key, r.PostForm[key], parse, def)
}

// FormSplit reads a single delimited body value, splitting on sep.
func FormSplit[T any](r *http.Request, key, sep string, def ...[]T) ([]T, error) {
	return resolveSplit(SourceForm, key, r.PostFormValue(key), sep, parse[T], def)
}

// FormSplitFunc is FormSplit with a caller-supplied element parser.
func FormSplitFunc[T any](r *http.Request, key, sep string, parse func(string) (T, error), def ...[]T) ([]T, error) {
	return resolveSplit(SourceForm, key, r.PostFormValue(key), sep, parse, def)
}

// HasForm reports whether body form field key is present.
func HasForm(r *http.Request, key string) bool {
	_ = r.ParseForm() // best-effort: a broken/absent body reads as no values (like net/http)
	return r.PostForm.Has(key)
}
