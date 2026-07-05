package middleware

import (
	"net/http"
	"slices"
)

// Middleware wraps an http.Handler with additional behavior. The FIRST Middleware
// passed to Chain/Wrap is the OUTERMOST layer: it sees the request first and the
// response last.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares into one. Chain(a, b, c) applied to h yields
// a(b(c(h))). An empty Chain is the identity wrapper.
func Chain(mws ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for _, mw := range slices.Backward(mws) {
			next = mw(next)
		}
		return next
	}
}

// Wrap applies mws to h, outermost first. Wrap(h) returns h unchanged.
func Wrap(h http.Handler, mws ...Middleware) http.Handler {
	return Chain(mws...)(h)
}

// When returns a Middleware that applies mw only to requests for which pred
// returns true; other requests pass to the next handler untouched. mw is built
// once per next, so a stateful middleware is constructed a single time.
func When(pred func(*http.Request) bool, mw Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pred(r) {
				wrapped.ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// Skip is the inverse of When: mw applies unless pred returns true.
func Skip(pred func(*http.Request) bool, mw Middleware) Middleware {
	return When(func(r *http.Request) bool { return !pred(r) }, mw)
}
