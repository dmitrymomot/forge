package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
)

func TestRecover(t *testing.T) {
	t.Parallel()

	t.Run("recovers from panic and returns PanicError", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			panic("test panic")
		})

		err := handler(ctx)
		require.Error(t, err)
		require.True(t, middlewares.IsPanicError(err))

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Equal(t, "test panic", pe.Value)
		require.NotEmpty(t, pe.Stack)
	})

	t.Run("passes through when no panic", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
	})

	t.Run("respects DisablePrintStack option", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover(middlewares.WithRecoverDisablePrintStack())
		handler := mw(func(c internal.Context) error {
			panic("test panic")
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Nil(t, pe.Stack)
	})
}

func TestPanicErrorHelpers(t *testing.T) {
	t.Parallel()

	t.Run("IsPanicError returns false for non-panic error", func(t *testing.T) {
		t.Parallel()

		err := http.ErrNoCookie
		require.False(t, middlewares.IsPanicError(err))
	})

	t.Run("AsPanicError returns false for non-panic error", func(t *testing.T) {
		t.Parallel()

		err := http.ErrNoCookie
		_, ok := middlewares.AsPanicError(err)
		require.False(t, ok)
	})
}
