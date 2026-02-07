package internal_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
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

// multipartRequest builds an *http.Request containing a single multipart form file.
func multipartRequest(t *testing.T, fieldName, fileName string, data []byte) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodGet, "/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestStorageNotConfigured(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	t.Run("Storage returns error when not configured", func(t *testing.T) {
		t.Parallel()

		requestVia(t, req, nil, func(c internal.Context) {
			s, err := c.Storage()
			require.Nil(t, s)
			require.ErrorIs(t, err, storage.ErrNotConfigured)
		})
	})

	t.Run("Upload returns error when not configured", func(t *testing.T) {
		t.Parallel()

		requestVia(t, req, nil, func(c internal.Context) {
			info, err := c.Upload("file")
			require.Nil(t, info)
			require.ErrorIs(t, err, storage.ErrNotConfigured)
		})
	})

	t.Run("UploadFromURL returns error when not configured", func(t *testing.T) {
		t.Parallel()

		requestVia(t, req, nil, func(c internal.Context) {
			info, err := c.UploadFromURL("https://example.com/image.png")
			require.Nil(t, info)
			require.ErrorIs(t, err, storage.ErrNotConfigured)
		})
	})

	t.Run("Download returns error when not configured", func(t *testing.T) {
		t.Parallel()

		requestVia(t, req, nil, func(c internal.Context) {
			rc, err := c.Download("test-key")
			require.Nil(t, rc)
			require.ErrorIs(t, err, storage.ErrNotConfigured)
		})
	})

	t.Run("DeleteFile returns error when not configured", func(t *testing.T) {
		t.Parallel()

		requestVia(t, req, nil, func(c internal.Context) {
			err := c.DeleteFile("test-key")
			require.ErrorIs(t, err, storage.ErrNotConfigured)
		})
	})

	t.Run("FileURL returns error when not configured", func(t *testing.T) {
		t.Parallel()

		requestVia(t, req, nil, func(c internal.Context) {
			url, err := c.FileURL("test-key")
			require.Empty(t, url)
			require.ErrorIs(t, err, storage.ErrNotConfigured)
		})
	})
}

func TestStorageConfigured(t *testing.T) {
	t.Parallel()

	mock := &mockStorage{}
	opts := []internal.Option{
		internal.WithStorage(mock),
	}

	t.Run("Storage returns configured client", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			s, err := c.Storage()
			require.NoError(t, err)
			require.Equal(t, mock, s)
		})
	})

	t.Run("Upload delegates to storage", func(t *testing.T) {
		t.Parallel()

		req := multipartRequest(t, "file", "hello.txt", []byte("test"))
		requestVia(t, req, opts, func(c internal.Context) {
			info, err := c.Upload("file")
			require.NoError(t, err)
			require.Equal(t, "test-key", info.Key)
		})
	})

	t.Run("Upload missing form field", func(t *testing.T) {
		t.Parallel()

		req := multipartRequest(t, "other", "hello.txt", []byte("test"))
		requestVia(t, req, opts, func(c internal.Context) {
			info, err := c.Upload("file")
			require.Nil(t, info)
			require.ErrorIs(t, err, http.ErrMissingFile)
		})
	})

	t.Run("Download delegates to storage", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			rc, err := c.Download("test-key")
			require.NoError(t, err)
			defer rc.Close()

			data, err := io.ReadAll(rc)
			require.NoError(t, err)
			require.Equal(t, "test content", string(data))
		})
	})

	t.Run("DeleteFile delegates to storage", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			err := c.DeleteFile("test-key")
			require.NoError(t, err)
		})
	})

	t.Run("FileURL delegates to storage", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			url, err := c.FileURL("test-key")
			require.NoError(t, err)
			require.Equal(t, "https://example.com/test-key", url)
		})
	})
}

func TestUploadWithOptions(t *testing.T) {
	t.Parallel()

	var receivedOpts []storage.Option

	mock := &mockStorage{
		putFn: func(_ context.Context, r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error) {
			receivedOpts = opts
			return &storage.FileInfo{Key: "test-key"}, nil
		},
	}
	opts := []internal.Option{internal.WithStorage(mock)}

	req := multipartRequest(t, "file", "photo.png", []byte("data"))
	requestVia(t, req, opts, func(c internal.Context) {
		info, err := c.Upload(
			"file",
			storage.WithContentType("image/png"),
			storage.WithPrefix("uploads"),
		)
		require.NoError(t, err)
		require.Equal(t, "test-key", info.Key)
	})

	// PutFile may add a WithContentType from MIME detection, so at least the two
	// caller-supplied options must be present.
	require.GreaterOrEqual(t, len(receivedOpts), 2, "Upload should forward storage options to Put")
}

func TestUploadFromURL(t *testing.T) {
	t.Parallel()

	t.Run("delegates to storage", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{}
		opts := []internal.Option{internal.WithStorage(mock)}

		// Serve a tiny file.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "hello")
		}))
		defer srv.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			info, err := c.UploadFromURL(srv.URL + "/file.txt")
			require.NoError(t, err)
			require.Equal(t, "test-key", info.Key)
		})
	})

	t.Run("invalid URL returns ErrInvalidURL", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{}
		opts := []internal.Option{internal.WithStorage(mock)}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			info, err := c.UploadFromURL("not-a-url")
			require.Nil(t, info)
			require.ErrorIs(t, err, storage.ErrInvalidURL)
		})
	})

	t.Run("propagates storage errors", func(t *testing.T) {
		t.Parallel()

		testErr := errors.New("storage error")
		mock := &mockStorage{
			putFn: func(_ context.Context, _ io.Reader, _ int64, _ ...storage.Option) (*storage.FileInfo, error) {
				return nil, testErr
			},
		}
		opts := []internal.Option{internal.WithStorage(mock)}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "hello")
		}))
		defer srv.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			info, err := c.UploadFromURL(srv.URL + "/file.txt")
			require.Nil(t, info)
			require.ErrorIs(t, err, testErr)
		})
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
		opts := []internal.Option{internal.WithStorage(mock)}

		req := multipartRequest(t, "file", "hello.txt", []byte("test"))
		requestVia(t, req, opts, func(c internal.Context) {
			info, err := c.Upload("file")
			require.Nil(t, info)
			require.ErrorIs(t, err, testErr)
		})
	})

	t.Run("Download propagates errors", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{
			getFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
				return nil, testErr
			},
		}
		opts := []internal.Option{internal.WithStorage(mock)}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			rc, err := c.Download("test-key")
			require.Nil(t, rc)
			require.ErrorIs(t, err, testErr)
		})
	})

	t.Run("DeleteFile propagates errors", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{
			deleteFn: func(ctx context.Context, key string) error {
				return testErr
			},
		}
		opts := []internal.Option{internal.WithStorage(mock)}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			err := c.DeleteFile("test-key")
			require.ErrorIs(t, err, testErr)
		})
	})

	t.Run("FileURL propagates errors", func(t *testing.T) {
		t.Parallel()

		mock := &mockStorage{
			urlFn: func(ctx context.Context, key string, opts ...storage.URLOption) (string, error) {
				return "", testErr
			},
		}
		opts := []internal.Option{internal.WithStorage(mock)}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			url, err := c.FileURL("test-key")
			require.Empty(t, url)
			require.ErrorIs(t, err, testErr)
		})
	})
}
