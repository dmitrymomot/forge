package middlewares_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
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

func TestRecover_PanicTypes(t *testing.T) {
	t.Parallel()

	t.Run("recovers from string panic", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			panic("string panic")
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Equal(t, "string panic", pe.Value)
	})

	t.Run("recovers from error panic", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		panicErr := errors.New("error panic")
		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			panic(panicErr)
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Equal(t, panicErr, pe.Value)
	})

	t.Run("recovers from integer panic", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			panic(42)
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Equal(t, 42, pe.Value)
	})

	t.Run("recovers from struct panic", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		type customError struct {
			Code    int
			Message string
		}
		panicValue := customError{Code: 500, Message: "custom"}

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			panic(panicValue)
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Equal(t, panicValue, pe.Value)
	})
}

func TestRecover_WithRecoverStackSize(t *testing.T) {
	t.Parallel()

	t.Run("custom stack size captures stack", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover(middlewares.WithRecoverStackSize(8192))
		handler := mw(func(c internal.Context) error {
			panic("test")
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.NotNil(t, pe.Stack)
		require.NotEmpty(t, pe.Stack)
	})

	t.Run("small stack size still captures partial trace", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover(middlewares.WithRecoverStackSize(100))
		handler := mw(func(c internal.Context) error {
			panic("test")
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.NotNil(t, pe.Stack)
		require.LessOrEqual(t, len(pe.Stack), 100)
	})

	t.Run("zero stack size still allocates default buffer", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover(middlewares.WithRecoverStackSize(0))
		handler := mw(func(c internal.Context) error {
			panic("test")
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		// With size 0, stack will be allocated but empty
		require.NotNil(t, pe.Stack)
	})
}

func TestRecover_CombinedOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithRecoverStackSize and WithRecoverDisablePrintStack together", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		// DisablePrintStack should take precedence
		mw := middlewares.Recover(
			middlewares.WithRecoverStackSize(8192),
			middlewares.WithRecoverDisablePrintStack(),
		)
		handler := mw(func(c internal.Context) error {
			panic("test")
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Nil(t, pe.Stack)
	})
}

func TestRecover_ErrorPropagation(t *testing.T) {
	t.Parallel()

	t.Run("handler error is returned without modification", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		expectedErr := errors.New("normal error")
		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			return expectedErr
		})

		err := handler(ctx)
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
		require.False(t, middlewares.IsPanicError(err))
	})

	t.Run("nil return is preserved", func(t *testing.T) {
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
}

func TestRecover_PanicNil(t *testing.T) {
	t.Parallel()

	t.Run("panic(nil) is caught as *runtime.PanicNilError", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			panic(nil) //nolint:govet // intentional: testing panic(nil) handling
		})

		err := handler(ctx)
		require.Error(t, err)
		require.True(t, middlewares.IsPanicError(err))

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		// Go 1.21+ wraps panic(nil) as *runtime.PanicNilError.
		require.IsType(t, (*runtime.PanicNilError)(nil), pe.Value)
	})
}

func TestRecover_DeferredPanic(t *testing.T) {
	t.Parallel()

	t.Run("catches panic from deferred function", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			defer func() {
				panic("deferred panic value")
			}()
			return nil
		})

		err := handler(ctx)
		require.Error(t, err)
		require.True(t, middlewares.IsPanicError(err))

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Equal(t, "deferred panic value", pe.Value)
		require.NotEmpty(t, pe.Stack)
	})
}

func TestRecover_NestedPanic(t *testing.T) {
	t.Parallel()

	t.Run("recovers from panic in nested function", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		nestedFunc := func() {
			panic("nested panic")
		}

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			nestedFunc()
			return nil
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.Equal(t, "nested panic", pe.Value)
		require.NotEmpty(t, pe.Stack)
	})

	t.Run("stack trace includes nested call frames", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		deepFunc := func() {
			panic("deep panic")
		}
		middleFunc := func() {
			deepFunc()
		}

		mw := middlewares.Recover()
		handler := mw(func(c internal.Context) error {
			middleFunc()
			return nil
		})

		err := handler(ctx)
		require.Error(t, err)

		pe, ok := middlewares.AsPanicError(err)
		require.True(t, ok)
		require.NotEmpty(t, pe.Stack)
		// Stack should contain function names
		require.Contains(t, string(pe.Stack), "middlewares_test")
	})
}
