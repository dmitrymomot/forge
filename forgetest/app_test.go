package forgetest

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
)

// testHandler wraps a HandlerFunc to implement forge.Handler.
type testHandler struct {
	fn forge.HandlerFunc
}

func (h *testHandler) Routes(r forge.Router) {
	r.GET("/test", h.fn)
}

func TestNewApp_BasicHandler(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			return c.String(http.StatusOK, "hello world")
		},
	}

	app := NewApp(t, handler)

	resp := Get(t, app, "/test").Do()

	resp.RequireStatus(t, http.StatusOK)
	require.Equal(t, "hello world", resp.Body())
}

func TestNewApp_StoreAccessible(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			return c.String(http.StatusOK, "ok")
		},
	}

	app := NewApp(t, handler)

	store := app.Store()
	require.NotNil(t, store)
	require.Equal(t, 0, store.Count())
}

func TestNewApp_WithRoles(t *testing.T) {
	t.Parallel()

	roles := forge.RolePermissions{
		"admin": {"users:delete", "users:create"},
		"user":  {"profile:edit"},
	}

	handler := &testHandler{
		fn: func(c forge.Context) error {
			canDelete := c.Can("users:delete")
			canEdit := c.Can("profile:edit")
			role := c.Role()

			if canDelete {
				return c.String(http.StatusOK, "admin:"+role)
			}
			if canEdit {
				return c.String(http.StatusOK, "user:"+role)
			}
			return c.String(http.StatusOK, "none:"+role)
		},
	}

	app := NewApp(t, handler, WithRoles(roles))

	t.Run("admin role has permissions", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-123").
			WithRole("admin").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "admin:admin", resp.Body())
	})

	t.Run("user role has limited permissions", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-456").
			WithRole("user").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "user:user", resp.Body())
	})

	t.Run("no role means no permissions", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-789").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "none:", resp.Body())
	})
}

func TestNewApp_WithMiddleware(t *testing.T) {
	t.Parallel()

	customMiddleware := func(next forge.HandlerFunc) forge.HandlerFunc {
		return func(c forge.Context) error {
			c.SetHeader("X-Custom-Header", "middleware-value")
			return next(c)
		}
	}

	handler := &testHandler{
		fn: func(c forge.Context) error {
			return c.String(http.StatusOK, "ok")
		},
	}

	app := NewApp(t, handler, WithMiddleware(customMiddleware))

	resp := Get(t, app, "/test").Do()

	resp.RequireStatus(t, http.StatusOK)
	resp.RequireHeader(t, "X-Custom-Header", "middleware-value")
}

func TestNewApp_WithErrorHandler(t *testing.T) {
	t.Parallel()

	customErrorHandler := func(c forge.Context, err error) error {
		return c.String(http.StatusInternalServerError, "custom error: "+err.Error())
	}

	handler := &testHandler{
		fn: func(c forge.Context) error {
			return c.Error(http.StatusBadRequest, "bad request happened")
		},
	}

	app := NewApp(t, handler, WithErrorHandler(customErrorHandler))

	resp := Get(t, app, "/test").Do()

	resp.RequireStatus(t, http.StatusInternalServerError)
	require.Contains(t, resp.Body(), "custom error:")
	require.Contains(t, resp.Body(), "bad request happened")
}

func TestNewApp_WithOption(t *testing.T) {
	t.Parallel()

	// Create a custom logger that discards output.
	customLogger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := &testHandler{
		fn: func(c forge.Context) error {
			// Verify logger is accessible and is our custom one.
			logger := c.Logger()
			require.NotNil(t, logger)
			return c.String(http.StatusOK, "ok")
		},
	}

	app := NewApp(t, handler, WithOption(forge.WithCustomLogger(customLogger)))

	resp := Get(t, app, "/test").Do()

	resp.RequireStatus(t, http.StatusOK)
	require.Equal(t, "ok", resp.Body())
}
