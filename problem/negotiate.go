package problem

import (
	"net/http"
	"strings"
)

// Negotiate returns a Responder that dispatches on the request Accept header: the
// first media range that exactly matches a key in byType uses that responder; a
// wildcard, an unmatched, or an absent Accept uses fallback. Q-values are not
// weighted — the standalone negotiate package handles full weighting.
//
//	problem.Negotiate(problem.JSON(), map[string]problem.Responder{
//		"text/html": problem.Component(errorPage),
//	})
func Negotiate(fallback Responder, byType map[string]Responder) Responder {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if resp := pick(r.Header.Get("Accept"), byType); resp != nil {
			resp(w, r, err)
			return
		}
		fallback(w, r, err)
	}
}

// pick returns the responder for the first Accept media range that exactly
// matches a key in byType, or nil when none match (the caller uses the fallback).
func pick(accept string, byType map[string]Responder) Responder {
	for part := range strings.SplitSeq(accept, ",") {
		mr := strings.TrimSpace(part)
		if i := strings.IndexByte(mr, ';'); i >= 0 {
			mr = strings.TrimSpace(mr[:i])
		}
		if resp, ok := byType[mr]; ok {
			return resp
		}
	}
	return nil
}
