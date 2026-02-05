package middlewares_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func TestTimeout_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("handler error returned before timeout", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("handler error")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Timeout(100 * time.Millisecond)
		handler := mw(func(c internal.Context) error {
			return expectedErr
		})

		err := handler(ctx)
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
		require.False(t, middlewares.IsTimeoutError(err))
	})

	t.Run("negative timeout uses default", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.Timeout(-5 * time.Second)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
	})

	t.Run("context cancelled for non-timeout reason", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		reqCtx, cancel := context.WithCancel(req.Context())
		req = req.WithContext(reqCtx)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		started := make(chan struct{})
		mw := middlewares.Timeout(1 * time.Second)
		handler := mw(func(c internal.Context) error {
			close(started)
			time.Sleep(200 * time.Millisecond)
			return nil
		})

		go func() {
			<-started
			cancel()
		}()

		err := handler(ctx)
		require.Error(t, err)
		// Should return context.Canceled, not TimeoutError
		require.True(t, errors.Is(err, context.Canceled))
		require.False(t, middlewares.IsTimeoutError(err))
	})
}

func TestGetTimeoutContext(t *testing.T) {
	t.Parallel()

	t.Run("returns timeout context when middleware applied", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		var timeoutCtx context.Context
		mw := middlewares.Timeout(100 * time.Millisecond)
		handler := mw(func(c internal.Context) error {
			timeoutCtx = middlewares.GetTimeoutContext(c)
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.NotNil(t, timeoutCtx)

		// Verify the timeout context has a deadline
		deadline, ok := timeoutCtx.Deadline()
		require.True(t, ok)
		require.True(t, deadline.After(time.Now()))
	})

	t.Run("returns request context when middleware not applied", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		// Call GetTimeoutContext without middleware
		timeoutCtx := middlewares.GetTimeoutContext(ctx)
		require.NotNil(t, timeoutCtx)
		require.Equal(t, ctx.Context(), timeoutCtx)
	})

	t.Run("returns request context when wrong type stored", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		// Store wrong type
		ctx.Set(struct{}{}, "not a context")

		timeoutCtx := middlewares.GetTimeoutContext(ctx)
		require.NotNil(t, timeoutCtx)
		require.Equal(t, ctx.Context(), timeoutCtx)
	})

	t.Run("timeout context cancels when deadline exceeded", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		var timeoutCtx atomic.Pointer[context.Context]
		mw := middlewares.Timeout(50 * time.Millisecond)
		handler := mw(func(c internal.Context) error {
			tc := middlewares.GetTimeoutContext(c)
			timeoutCtx.Store(&tc)
			time.Sleep(100 * time.Millisecond)
			return nil
		})

		err := handler(ctx)
		require.Error(t, err)
		require.True(t, middlewares.IsTimeoutError(err))

		// Verify the timeout context is cancelled
		tc := timeoutCtx.Load()
		require.NotNil(t, tc)
		require.Error(t, (*tc).Err())
		require.Equal(t, context.DeadlineExceeded, (*tc).Err())
	})

	t.Run("handler can detect cancellation via timeout context", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		var cancelled atomic.Bool
		mw := middlewares.Timeout(20 * time.Millisecond)
		handler := mw(func(c internal.Context) error {
			timeoutCtx := middlewares.GetTimeoutContext(c)

			// Simulate long-running operation that checks for cancellation
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()

			for range 10 {
				select {
				case <-timeoutCtx.Done():
					cancelled.Store(true)
					return nil
				case <-ticker.C:
					// Continue working
				}
			}
			return nil
		})

		err := handler(ctx)
		require.True(t, middlewares.IsTimeoutError(err) || cancelled.Load())
	})
}

func TestTimeout_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	t.Run("multiple concurrent requests with different timeouts", func(t *testing.T) {
		t.Parallel()

		mw := middlewares.Timeout(50 * time.Millisecond)

		// Fast request
		req1 := httptest.NewRequest(http.MethodGet, "/fast", nil)
		rec1 := httptest.NewRecorder()
		ctx1 := newTestContext(rec1, req1)

		// Slow request
		req2 := httptest.NewRequest(http.MethodGet, "/slow", nil)
		rec2 := httptest.NewRecorder()
		ctx2 := newTestContext(rec2, req2)

		done1 := make(chan error, 1)
		done2 := make(chan error, 1)

		go func() {
			handler := mw(func(c internal.Context) error {
				time.Sleep(10 * time.Millisecond)
				return nil
			})
			done1 <- handler(ctx1)
		}()

		go func() {
			handler := mw(func(c internal.Context) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			done2 <- handler(ctx2)
		}()

		err1 := <-done1
		err2 := <-done2

		require.NoError(t, err1)
		require.Error(t, err2)
		require.True(t, middlewares.IsTimeoutError(err2))
	})
}
