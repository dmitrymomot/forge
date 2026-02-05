package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	t.Run("generates new request ID when not present", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID()
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, rec.Header().Get("X-Request-ID"))
	})

	t.Run("uses existing request ID from header", func(t *testing.T) {
		t.Parallel()

		existingID := "existing-request-id-123"
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", existingID)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID()
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, existingID, rec.Header().Get("X-Request-ID"))
	})

	t.Run("GetRequestID returns stored ID", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		var capturedID string
		mw := middlewares.RequestID()
		handler := mw(func(c internal.Context) error {
			capturedID = middlewares.GetRequestID(ctx)
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, capturedID)
		require.Equal(t, capturedID, rec.Header().Get("X-Request-ID"))
	})
}

func TestRequestIDExtractor(t *testing.T) {
	t.Parallel()

	t.Run("returns attribute when request ID present", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID()
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)

		extractor := middlewares.RequestIDExtractor()
		attr, ok := extractor(ctx.Context())
		require.True(t, ok)
		require.Equal(t, "request_id", attr.Key)
		require.NotEmpty(t, attr.Value.String())
	})
}

func TestRequestID_CustomOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithRequestIDHeaders uses custom headers in priority order", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Custom-ID", "custom-123")
		req.Header.Set("X-Trace-ID", "trace-456")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID(
			middlewares.WithRequestIDHeaders("X-Custom-ID", "X-Trace-ID"),
		)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, "custom-123", rec.Header().Get("X-Request-ID"))
	})

	t.Run("WithRequestIDHeaders respects priority order", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Trace-ID", "trace-456")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID(
			middlewares.WithRequestIDHeaders("X-Custom-ID", "X-Trace-ID"),
		)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, "trace-456", rec.Header().Get("X-Request-ID"))
	})

	t.Run("WithRequestIDGenerator uses custom generator", func(t *testing.T) {
		t.Parallel()

		customID := "custom-generated-id"
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID(
			middlewares.WithRequestIDGenerator(func() string {
				return customID
			}),
		)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, customID, rec.Header().Get("X-Request-ID"))
	})

	t.Run("WithRequestIDResponseHeader sets custom response header", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID(
			middlewares.WithRequestIDResponseHeader("X-Custom-Response-ID"),
		)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, rec.Header().Get("X-Custom-Response-ID"))
		require.Empty(t, rec.Header().Get("X-Request-ID"))
	})

	t.Run("multiple options work together", func(t *testing.T) {
		t.Parallel()

		customID := "generated-123"
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID(
			middlewares.WithRequestIDHeaders("X-Trace-ID", "X-Request-ID"),
			middlewares.WithRequestIDGenerator(func() string { return customID }),
			middlewares.WithRequestIDResponseHeader("X-Response-ID"),
		)
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, customID, rec.Header().Get("X-Response-ID"))
	})
}

func TestRequestID_HeaderPriority(t *testing.T) {
	t.Parallel()

	t.Run("uses first matching header when multiple present", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		// Note: HTTP headers are canonicalized, so X-Request-ID and X-Request-Id
		// both map to the same canonical form. We test with different header names.
		req.Header.Set("X-Request-ID", "req-123")
		req.Header.Set("X-Correlation-ID", "corr-789")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID()
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, "req-123", rec.Header().Get("X-Request-ID"))
	})

	t.Run("falls back to second header when first empty", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-Id", "req-456")
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		mw := middlewares.RequestID()
		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(ctx)
		require.NoError(t, err)
		require.Equal(t, "req-456", rec.Header().Get("X-Request-ID"))
	})
}

func TestGetRequestID_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("returns empty string when no request ID set", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		// Call GetRequestID without middleware
		id := middlewares.GetRequestID(ctx)
		require.Empty(t, id)
	})

	t.Run("returns empty string when context has wrong type", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		// Set a non-string value
		ctx.Set(struct{}{}, 123)

		id := middlewares.GetRequestID(ctx)
		require.Empty(t, id)
	})
}

func TestRequestIDExtractor_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("returns false when no request ID in context", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		extractor := middlewares.RequestIDExtractor()
		_, ok := extractor(ctx.Context())
		require.False(t, ok)
	})

	t.Run("returns false when request ID is empty string", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		// Manually set empty string
		ctx.Set(struct{}{}, "")

		extractor := middlewares.RequestIDExtractor()
		_, ok := extractor(ctx.Context())
		require.False(t, ok)
	})

	t.Run("returns false when context value is wrong type", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ctx := newTestContext(rec, req)

		// Set wrong type
		ctx.Set(struct{}{}, 123)

		extractor := middlewares.RequestIDExtractor()
		_, ok := extractor(ctx.Context())
		require.False(t, ok)
	})
}
