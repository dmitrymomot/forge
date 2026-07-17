package tenant

import (
	"net/http"
	"slices"
)

// Middleware resolves the tenant for each request through resolvers in
// precedence order: the first resolver returning a non-empty ID wins and is
// stamped on the request context via NewContext, overriding any tenant
// already there (give a pre-existing context tenant an explicit slot with
// the Context resolver). A resolver error responds 500 with a generic body
// and does not call next. When nothing resolves, next runs untenanted so
// single-tenant routes coexist; add Require where tenancy is mandatory.
//
// The returned wrapper satisfies web/middleware.Middleware structurally.
// Panics with ErrNilResolver when any resolver is nil.
func Middleware(resolvers ...Resolver) func(http.Handler) http.Handler {
	for _, resolve := range resolvers {
		if resolve == nil {
			panic(ErrNilResolver)
		}
	}
	chain := slices.Clone(resolvers)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, resolve := range chain {
				id, err := resolve(r)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				if id != "" {
					r = r.WithContext(NewContext(r.Context(), id))
					break
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Require is a guard middleware responding 404 Not Found when the request
// context carries no tenant. 404 — not 401/403 — because an unresolved
// tenant host has nothing there, and the status leaks nothing about
// tenancy. Place it after Middleware on routes where tenancy is mandatory:
//
//	middleware.Chain(tenant.Middleware(resolvers...), tenant.Require)
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
