package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
)

// authTestContext wraps testContext with configurable auth and permission behavior.
type authTestContext struct {
	*testContext
	authenticated bool
	permissions   map[internal.Permission]bool
	role          string
}

func (c *authTestContext) IsAuthenticated() bool {
	return c.authenticated
}

func (c *authTestContext) Can(permission internal.Permission) bool {
	return c.permissions[permission]
}

func (c *authTestContext) Role() string {
	return c.role
}

// runAuthMiddleware executes a middleware with the given authTestContext and returns
// true if the handler was called (middleware passed through), along with the error.
func runAuthMiddleware(t *testing.T, mw internal.Middleware, ctx *authTestContext) (passed bool, err error) {
	t.Helper()
	handler := mw(func(c internal.Context) error {
		passed = true
		return nil
	})
	err = handler(ctx)
	return passed, err
}

func newAuthTestContext(authenticated bool, perms map[internal.Permission]bool) *authTestContext {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return &authTestContext{
		testContext:   newTestContext(w, r),
		authenticated: authenticated,
		permissions:   perms,
	}
}

func TestRequireAuthenticated(t *testing.T) {
	t.Parallel()

	t.Run("authenticated user passes through", func(t *testing.T) {
		t.Parallel()

		ctx := newAuthTestContext(true, nil)
		passed, err := runAuthMiddleware(t, middlewares.RequireAuthenticated(), ctx)
		require.NoError(t, err)
		require.True(t, passed)
	})

	t.Run("unauthenticated user gets 401", func(t *testing.T) {
		t.Parallel()

		ctx := newAuthTestContext(false, nil)
		passed, err := runAuthMiddleware(t, middlewares.RequireAuthenticated(), ctx)
		require.False(t, passed)
		require.Error(t, err)

		httpErr := internal.AsHTTPError(err)
		require.NotNil(t, httpErr)
		require.Equal(t, http.StatusUnauthorized, httpErr.Code)
	})
}
