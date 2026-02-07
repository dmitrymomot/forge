package middlewares

import "github.com/dmitrymomot/forge/internal"

// RequireAuthenticated returns a middleware that rejects unauthenticated requests
// with 401 Unauthorized. It checks Context.IsAuthenticated().
func RequireAuthenticated() internal.Middleware {
	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			if !c.IsAuthenticated() {
				return internal.ErrUnauthorized("authentication required")
			}
			return next(c)
		}
	}
}
