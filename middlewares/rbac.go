package middlewares

import (
	"slices"

	"github.com/dmitrymomot/forge/internal"
)

// RequirePermission returns a middleware that requires the user to have ALL of
// the listed permissions. Unauthenticated users receive 401; authenticated users
// without the required permissions receive 403.
//
// Panics if no permissions are provided (programmer error).
func RequirePermission(perms ...internal.Permission) internal.Middleware {
	if len(perms) == 0 {
		panic("middlewares.RequirePermission: at least one permission is required")
	}
	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			if !c.IsAuthenticated() {
				return internal.ErrUnauthorized("authentication required")
			}
			for _, p := range perms {
				if !c.Can(p) {
					return internal.ErrForbidden("insufficient permissions")
				}
			}
			return next(c)
		}
	}
}

// RequireAnyPermission returns a middleware that requires the user to have at
// least ONE of the listed permissions. Unauthenticated users receive 401;
// authenticated users without any of the required permissions receive 403.
//
// Panics if no permissions are provided (programmer error).
func RequireAnyPermission(perms ...internal.Permission) internal.Middleware {
	if len(perms) == 0 {
		panic("middlewares.RequireAnyPermission: at least one permission is required")
	}
	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			if !c.IsAuthenticated() {
				return internal.ErrUnauthorized("authentication required")
			}
			if slices.ContainsFunc(perms, func(p internal.Permission) bool {
				return c.Can(p)
			}) {
				return next(c)
			}
			return internal.ErrForbidden("insufficient permissions")
		}
	}
}
