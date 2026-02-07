package internal_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
)

func TestResponseWriterSizeTracking(t *testing.T) {
	t.Parallel()

	t.Run("tracks bytes written", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		rw := internal.NewResponseWriter(rec, false)

		n1, err := rw.Write([]byte("hello"))
		require.NoError(t, err)
		require.Equal(t, 5, n1)
		require.Equal(t, int64(5), rw.Size())

		n2, err := rw.Write([]byte(" world"))
		require.NoError(t, err)
		require.Equal(t, 6, n2)
		require.Equal(t, int64(11), rw.Size())
	})

	t.Run("size starts at zero", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		rw := internal.NewResponseWriter(rec, false)
		require.Equal(t, int64(0), rw.Size())
	})

	t.Run("status defaults to 200", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		rw := internal.NewResponseWriter(rec, false)
		require.Equal(t, http.StatusOK, rw.Status())
	})

	t.Run("status reflects WriteHeader", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		rw := internal.NewResponseWriter(rec, false)
		rw.WriteHeader(http.StatusNotFound)
		require.Equal(t, http.StatusNotFound, rw.Status())
	})
}

func TestResponseWriterConcurrentAccess(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rw := internal.NewResponseWriter(rec, false)

	var wg sync.WaitGroup
	const iterations = 100

	// Writer goroutine
	wg.Go(func() {
		for range iterations {
			_, _ = rw.Write([]byte("x"))
		}
	})

	// Reader goroutine
	wg.Go(func() {
		for range iterations {
			_ = rw.Size()
			_ = rw.Status()
		}
	})

	wg.Wait()
	require.Equal(t, int64(iterations), rw.Size())
}
