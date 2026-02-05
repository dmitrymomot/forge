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
