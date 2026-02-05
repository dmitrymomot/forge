package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
)

func TestTimeout(t *testing.T) {
	t.Parallel()

	t.Run("passes through when handler completes in time", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Timeout(100 * time.Millisecond)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
	})

	t.Run("returns TimeoutError when handler exceeds timeout", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Timeout(10 * time.Millisecond)
		handler := mw(func(c internal.Context) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		})

		err := handler(ctx)
		require.Error(t, err)
		require.True(t, middlewares.IsTimeoutError(err))

		te, ok := middlewares.AsTimeoutError(err)
		require.True(t, ok)
		require.Equal(t, 10*time.Millisecond, te.Duration)
	})

	t.Run("uses default timeout when zero provided", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Timeout(0)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
	})
}

func TestTimeoutErrorHelpers(t *testing.T) {
	t.Parallel()

	t.Run("IsTimeoutError returns false for non-timeout error", func(t *testing.T) {
		t.Parallel()

		err := http.ErrNoCookie
		require.False(t, middlewares.IsTimeoutError(err))
	})

	t.Run("AsTimeoutError returns false for non-timeout error", func(t *testing.T) {
		t.Parallel()

		err := http.ErrNoCookie
		_, ok := middlewares.AsTimeoutError(err)
		require.False(t, ok)
	})
}
