package internal_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
)

func TestIsHTTPError(t *testing.T) {
	t.Parallel()

	t.Run("direct HTTPError", func(t *testing.T) {
		t.Parallel()
		err := internal.NewHTTPError(http.StatusNotFound, "not found")
		require.True(t, internal.IsHTTPError(err))
	})

	t.Run("wrapped HTTPError", func(t *testing.T) {
		t.Parallel()
		httpErr := internal.NewHTTPError(http.StatusBadRequest, "bad request")
		err := fmt.Errorf("handler failed: %w", httpErr)
		require.True(t, internal.IsHTTPError(err))
	})

	t.Run("double-wrapped HTTPError", func(t *testing.T) {
		t.Parallel()
		httpErr := internal.NewHTTPError(http.StatusConflict, "conflict")
		err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", httpErr))
		require.True(t, internal.IsHTTPError(err))
	})

	t.Run("unrelated error", func(t *testing.T) {
		t.Parallel()
		err := errors.New("something went wrong")
		require.False(t, internal.IsHTTPError(err))
	})

	t.Run("nil error", func(t *testing.T) {
		t.Parallel()
		require.False(t, internal.IsHTTPError(nil))
	})
}

func TestAsHTTPError(t *testing.T) {
	t.Parallel()

	t.Run("direct HTTPError", func(t *testing.T) {
		t.Parallel()
		httpErr := internal.NewHTTPError(http.StatusNotFound, "not found")
		got := internal.AsHTTPError(httpErr)
		require.NotNil(t, got)
		require.Equal(t, http.StatusNotFound, got.Code)
		require.Equal(t, "not found", got.Message)
	})

	t.Run("wrapped HTTPError preserves fields", func(t *testing.T) {
		t.Parallel()
		httpErr := internal.NewHTTPError(http.StatusForbidden, "forbidden")
		httpErr.Title = "Access Denied"
		httpErr.ErrorCode = "AUTH_001"
		err := fmt.Errorf("middleware: %w", httpErr)

		got := internal.AsHTTPError(err)
		require.NotNil(t, got)
		require.Equal(t, http.StatusForbidden, got.Code)
		require.Equal(t, "forbidden", got.Message)
		require.Equal(t, "Access Denied", got.Title)
		require.Equal(t, "AUTH_001", got.ErrorCode)
	})

	t.Run("unrelated error returns nil", func(t *testing.T) {
		t.Parallel()
		err := errors.New("plain error")
		require.Nil(t, internal.AsHTTPError(err))
	})

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, internal.AsHTTPError(nil))
	})
}
