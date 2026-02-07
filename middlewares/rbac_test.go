package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
)

// rbacTestContext wraps testContext with configurable auth and permission behavior.
type rbacTestContext struct {
	*testContext
	authenticated bool
	permissions   map[internal.Permission]bool
	role          string
}

func (c *rbacTestContext) IsAuthenticated() bool {
	return c.authenticated
}

func (c *rbacTestContext) Can(permission internal.Permission) bool {
	return c.permissions[permission]
}

func (c *rbacTestContext) Role() string {
	return c.role
}

// runRBACMiddleware executes a middleware with the given rbacTestContext and returns
// true if the handler was called (middleware passed through), along with the error.
func runRBACMiddleware(t *testing.T, mw internal.Middleware, ctx *rbacTestContext) (passed bool, err error) {
	t.Helper()
	handler := mw(func(c internal.Context) error {
		passed = true
		return nil
	})
	err = handler(ctx)
	return passed, err
}

func newRBACTestContext(authenticated bool, perms map[internal.Permission]bool) *rbacTestContext {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return &rbacTestContext{
		testContext:   newTestContext(w, r),
		authenticated: authenticated,
		permissions:   perms,
	}
}

func TestRequirePermission(t *testing.T) {
	t.Parallel()

	t.Run("user has all permissions", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"billing.read":  true,
			"billing.write": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequirePermission("billing.read", "billing.write"), ctx)
		require.NoError(t, err)
		require.True(t, passed)
	})

	t.Run("user missing one permission", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"billing.read": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequirePermission("billing.read", "billing.write"), ctx)
		require.False(t, passed)

		httpErr := internal.AsHTTPError(err)
		require.NotNil(t, httpErr)
		require.Equal(t, http.StatusForbidden, httpErr.Code)
	})

	t.Run("unauthenticated user gets 401 not 403", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(false, map[internal.Permission]bool{
			"billing.read": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequirePermission("billing.read"), ctx)
		require.False(t, passed)

		httpErr := internal.AsHTTPError(err)
		require.NotNil(t, httpErr)
		require.Equal(t, http.StatusUnauthorized, httpErr.Code)
	})

	t.Run("single permission user has it", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"users.read": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequirePermission("users.read"), ctx)
		require.NoError(t, err)
		require.True(t, passed)
	})

	t.Run("zero args panics", func(t *testing.T) {
		t.Parallel()

		require.Panics(t, func() {
			middlewares.RequirePermission()
		})
	})

	t.Run("user has no permissions at all", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, nil)
		passed, err := runRBACMiddleware(t, middlewares.RequirePermission("billing.read"), ctx)
		require.False(t, passed)

		httpErr := internal.AsHTTPError(err)
		require.NotNil(t, httpErr)
		require.Equal(t, http.StatusForbidden, httpErr.Code)
	})
}

func TestRequireAnyPermission(t *testing.T) {
	t.Parallel()

	t.Run("user has one of required", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"users.read": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequireAnyPermission("users.read", "users.admin"), ctx)
		require.NoError(t, err)
		require.True(t, passed)
	})

	t.Run("user has all of required", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"users.read":  true,
			"users.admin": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequireAnyPermission("users.read", "users.admin"), ctx)
		require.NoError(t, err)
		require.True(t, passed)
	})

	t.Run("user has none", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"billing.read": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequireAnyPermission("users.read", "users.admin"), ctx)
		require.False(t, passed)

		httpErr := internal.AsHTTPError(err)
		require.NotNil(t, httpErr)
		require.Equal(t, http.StatusForbidden, httpErr.Code)
	})

	t.Run("unauthenticated user gets 401 not 403", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(false, nil)
		passed, err := runRBACMiddleware(t, middlewares.RequireAnyPermission("users.read"), ctx)
		require.False(t, passed)

		httpErr := internal.AsHTTPError(err)
		require.NotNil(t, httpErr)
		require.Equal(t, http.StatusUnauthorized, httpErr.Code)
	})

	t.Run("zero args panics", func(t *testing.T) {
		t.Parallel()

		require.Panics(t, func() {
			middlewares.RequireAnyPermission()
		})
	})

	t.Run("single permission user has it", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"users.read": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequireAnyPermission("users.read"), ctx)
		require.NoError(t, err)
		require.True(t, passed)
	})

	t.Run("last permission in list matches", func(t *testing.T) {
		t.Parallel()

		ctx := newRBACTestContext(true, map[internal.Permission]bool{
			"billing.manage": true,
		})
		passed, err := runRBACMiddleware(t, middlewares.RequireAnyPermission("users.read", "users.admin", "billing.manage"), ctx)
		require.NoError(t, err)
		require.True(t, passed)
	})
}
