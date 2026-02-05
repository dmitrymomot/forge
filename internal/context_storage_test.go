package internal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/logger"
	"github.com/dmitrymomot/forge/pkg/storage"
)

// mockStorage implements storage.Storage for testing.
type mockStorage struct {
	putFn    func(ctx context.Context, r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error)
	getFn    func(ctx context.Context, key string) (io.ReadCloser, error)
	deleteFn func(ctx context.Context, key string) error
	urlFn    func(ctx context.Context, key string, opts ...storage.URLOption) (string, error)
}

func (m *mockStorage) Put(ctx context.Context, r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error) {
	if m.putFn != nil {
		return m.putFn(ctx, r, size, opts...)
	}
	return &storage.FileInfo{Key: "test-key"}, nil
}

func (m *mockStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return io.NopCloser(bytes.NewReader([]byte("test content"))), nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	return nil
}

func (m *mockStorage) URL(ctx context.Context, key string, opts ...storage.URLOption) (string, error) {
	if m.urlFn != nil {
		return m.urlFn(ctx, key, opts...)
	}
	return "https://example.com/" + key, nil
}

func TestStorageNotConfigured(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	c := newContext(w, req, logger.NewNope(), cookie.New(), nil, nil, nil, "")

	t.Run("Storage returns error when not configured", func(t *testing.T) {
		t.Parallel()

		s, err := c.Storage()
		require.Nil(t, s)
		require.ErrorIs(t, err, storage.ErrNotConfigured)
	})

	t.Run("Upload returns error when not configured", func(t *testing.T) {
		t.Parallel()

		info, err := c.Upload(bytes.NewReader([]byte("test")), 4)
		require.Nil(t, info)
		require.ErrorIs(t, err, storage.ErrNotConfigured)
	})

	t.Run("Download returns error when not configured", func(t *testing.T) {
		t.Parallel()

		rc, err := c.Download("test-key")
		require.Nil(t, rc)
		require.ErrorIs(t, err, storage.ErrNotConfigured)
	})

	t.Run("DeleteFile returns error when not configured", func(t *testing.T) {
		t.Parallel()

		err := c.DeleteFile("test-key")
		require.ErrorIs(t, err, storage.ErrNotConfigured)
	})

	t.Run("FileURL returns error when not configured", func(t *testing.T) {
		t.Parallel()

		url, err := c.FileURL("test-key")
		require.Empty(t, url)
		require.ErrorIs(t, err, storage.ErrNotConfigured)
	})
}

func TestStorageConfigured(t *testing.T) {
	t.Parallel()

	mock := &mockStorage{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	c := newContext(w, req, logger.NewNope(), cookie.New(), nil, nil, mock, "")

	t.Run("Storage returns configured client", func(t *testing.T) {
		t.Parallel()

		s, err := c.Storage()
		require.NoError(t, err)
		require.Equal(t, mock, s)
	})

	t.Run("Upload delegates to storage", func(t *testing.T) {
		t.Parallel()

		info, err := c.Upload(bytes.NewReader([]byte("test")), 4)
		require.NoError(t, err)
		require.Equal(t, "test-key", info.Key)
	})

	t.Run("Download delegates to storage", func(t *testing.T) {
		t.Parallel()

		rc, err := c.Download("test-key")
		require.NoError(t, err)
		defer rc.Close()

		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, "test content", string(data))
	})

	t.Run("DeleteFile delegates to storage", func(t *testing.T) {
		t.Parallel()

		err := c.DeleteFile("test-key")
		require.NoError(t, err)
	})

	t.Run("FileURL delegates to storage", func(t *testing.T) {
		t.Parallel()

		url, err := c.FileURL("test-key")
		require.NoError(t, err)
		require.Equal(t, "https://example.com/test-key", url)
	})
}

func TestStorageErrors(t *testing.T) {
	t.Parallel()

	testErr := errors.New("storage error")

	t.Run("Upload propagates errors", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{
			putFn: func(ctx context.Context, r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error) {
				return nil, testErr
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newContext(w, req, logger.NewNope(), cookie.New(), nil, nil, mock, "")

		info, err := c.Upload(bytes.NewReader([]byte("test")), 4)
		require.Nil(t, info)
		require.ErrorIs(t, err, testErr)
	})

	t.Run("Download propagates errors", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{
			getFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
				return nil, testErr
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newContext(w, req, logger.NewNope(), cookie.New(), nil, nil, mock, "")

		rc, err := c.Download("test-key")
		require.Nil(t, rc)
		require.ErrorIs(t, err, testErr)
	})

	t.Run("DeleteFile propagates errors", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{
			deleteFn: func(ctx context.Context, key string) error {
				return testErr
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newContext(w, req, logger.NewNope(), cookie.New(), nil, nil, mock, "")

		err := c.DeleteFile("test-key")
		require.ErrorIs(t, err, testErr)
	})

	t.Run("FileURL propagates errors", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{
			urlFn: func(ctx context.Context, key string, opts ...storage.URLOption) (string, error) {
				return "", testErr
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newContext(w, req, logger.NewNope(), cookie.New(), nil, nil, mock, "")

		url, err := c.FileURL("test-key")
		require.Empty(t, url)
		require.ErrorIs(t, err, testErr)
	})
}
