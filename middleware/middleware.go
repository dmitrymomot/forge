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
