package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockStorage is a test implementation of the Storage interface.
type mockStorage struct {
	putFunc    func(ctx context.Context, r io.Reader, size int64, opts ...Option) (*FileInfo, error)
	getFunc    func(ctx context.Context, key string) (io.ReadCloser, error)
	deleteFunc func(ctx context.Context, key string) error
	urlFunc    func(ctx context.Context, key string, opts ...URLOption) (string, error)
}

func (m *mockStorage) Put(ctx context.Context, r io.Reader, size int64, opts ...Option) (*FileInfo, error) {
	if m.putFunc != nil {
		return m.putFunc(ctx, r, size, opts...)
	}
	return &FileInfo{Key: "test-key", Size: size}, nil
}

func (m *mockStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, key)
	}
	return io.NopCloser(strings.NewReader("test")), nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, key)
	}
	return nil
}

func (m *mockStorage) URL(ctx context.Context, key string, opts ...URLOption) (string, error) {
	if m.urlFunc != nil {
		return m.urlFunc(ctx, key, opts...)
	}
	return "https://example.com/" + key, nil
}

// mockMultipartFile creates a multipart.FileHeader backed by actual data.
func mockMultipartFile(t *testing.T, filename string, data []byte) *multipart.FileHeader {
	t.Helper()

	// Create a pipe to simulate file content
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	// Parse the multipart form
	reader := multipart.NewReader(body, writer.Boundary())
	form, err := reader.ReadForm(int64(len(data)) + 1024)
	require.NoError(t, err)

	files := form.File["file"]
	require.Len(t, files, 1)
	return files[0]
}

// TestPutFile tests the PutFile helper function.
func TestPutFile(t *testing.T) {
	t.Parallel()

	t.Run("nil file header returns ErrEmptyFile", func(t *testing.T) {
		t.Parallel()

		storage := &mockStorage{}
		_, err := PutFile(context.Background(), storage, nil)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrEmptyFile))
	})

	t.Run("zero size file returns ErrEmptyFile", func(t *testing.T) {
		t.Parallel()

		fh := &multipart.FileHeader{
			Filename: "test.txt",
			Size:     0,
			Header:   textproto.MIMEHeader{},
		}
		storage := &mockStorage{}
		_, err := PutFile(context.Background(), storage, fh)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrEmptyFile))
	})

	t.Run("successful upload without validation", func(t *testing.T) {
		t.Parallel()

		data := []byte("hello world")
		fh := mockMultipartFile(t, "test.txt", data)

		var capturedSize int64
		storage := &mockStorage{
			putFunc: func(_ context.Context, r io.Reader, size int64, _ ...Option) (*FileInfo, error) {
				capturedSize = size
				content, err := io.ReadAll(r)
				require.NoError(t, err)
				require.Equal(t, data, content)
				return &FileInfo{Key: "test-key", Size: size, ContentType: "text/plain"}, nil
			},
		}

		info, err := PutFile(context.Background(), storage, fh)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "test-key", info.Key)
		require.Equal(t, int64(len(data)), capturedSize)
	})

	t.Run("validation failure returns FileValidationError", func(t *testing.T) {
		t.Parallel()

		data := []byte("hello world")
		fh := mockMultipartFile(t, "test.txt", data)

		storage := &mockStorage{}
		_, err := PutFile(context.Background(), storage, fh,
			WithValidation(MaxSize(5)), // File is 11 bytes, limit is 5
		)
		require.Error(t, err)
		var verr *FileValidationError
		require.True(t, errors.As(err, &verr))
		require.Equal(t, ErrCodeFileTooLarge, verr.Code)
	})

	t.Run("successful upload with validation", func(t *testing.T) {
		t.Parallel()

		// PNG magic bytes followed by minimal data
		pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		pngData = append(pngData, make([]byte, 100)...)
		fh := mockMultipartFile(t, "test.png", pngData)

		var capturedContentType string
		storage := &mockStorage{
			putFunc: func(_ context.Context, _ io.Reader, size int64, opts ...Option) (*FileInfo, error) {
				o := &putOptions{}
				for _, opt := range opts {
					opt(o)
				}
				capturedContentType = o.contentType
				return &FileInfo{Key: "test-key", Size: size, ContentType: o.contentType}, nil
			},
		}

		info, err := PutFile(context.Background(), storage, fh,
			WithValidation(
				MaxSize(1<<20), // 1MB limit
				ImageOnly(),
			),
		)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "image/png", capturedContentType)
	})

	t.Run("validation with type mismatch", func(t *testing.T) {
		t.Parallel()

		data := []byte("plain text content")
		fh := mockMultipartFile(t, "test.txt", data)

		storage := &mockStorage{}
		_, err := PutFile(context.Background(), storage, fh,
			WithValidation(ImageOnly()),
		)
		require.Error(t, err)
		var verr *FileValidationError
		require.True(t, errors.As(err, &verr))
		require.Equal(t, ErrCodeInvalidMIME, verr.Code)
	})

	t.Run("storage error propagates", func(t *testing.T) {
		t.Parallel()

		data := []byte("hello world")
		fh := mockMultipartFile(t, "test.txt", data)

		storageErr := errors.New("storage unavailable")
		storage := &mockStorage{
			putFunc: func(_ context.Context, _ io.Reader, _ int64, _ ...Option) (*FileInfo, error) {
				return nil, storageErr
			},
		}

		_, err := PutFile(context.Background(), storage, fh)
		require.Error(t, err)
		require.True(t, errors.Is(err, storageErr))
	})

	t.Run("file open error returns wrapped error", func(t *testing.T) {
		t.Parallel()

		// Create a FileHeader that will fail to open because it has no content
		// (no multipart form backing it).
		fh := &multipart.FileHeader{
			Filename: "test.txt",
			Size:     100,
			Header:   textproto.MIMEHeader{},
		}

		storage := &mockStorage{}
		_, err := PutFile(context.Background(), storage, fh)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to open file")
	})
}

// TestPutBytes tests the PutBytes helper function.
func TestPutBytes(t *testing.T) {
	t.Parallel()

	t.Run("empty data returns ErrEmptyFile", func(t *testing.T) {
		t.Parallel()

		storage := &mockStorage{}
		_, err := PutBytes(context.Background(), storage, []byte{}, "test.txt")
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrEmptyFile))
	})

	t.Run("nil data returns ErrEmptyFile", func(t *testing.T) {
		t.Parallel()

		storage := &mockStorage{}
		_, err := PutBytes(context.Background(), storage, nil, "test.txt")
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrEmptyFile))
	})

	t.Run("successful upload", func(t *testing.T) {
		t.Parallel()

		data := []byte("hello world")
		var capturedSize int64
		var capturedData []byte

		storage := &mockStorage{
			putFunc: func(_ context.Context, r io.Reader, size int64, _ ...Option) (*FileInfo, error) {
				capturedSize = size
				var err error
				capturedData, err = io.ReadAll(r)
				require.NoError(t, err)
				return &FileInfo{Key: "test-key", Size: size}, nil
			},
		}

		info, err := PutBytes(context.Background(), storage, data, "test.txt")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, int64(len(data)), capturedSize)
		require.Equal(t, data, capturedData)
	})

	t.Run("large data upload", func(t *testing.T) {
		t.Parallel()

		data := make([]byte, 1<<20) // 1MB
		for i := range data {
			data[i] = byte(i % 256)
		}

		storage := &mockStorage{
			putFunc: func(_ context.Context, r io.Reader, size int64, _ ...Option) (*FileInfo, error) {
				content, err := io.ReadAll(r)
				require.NoError(t, err)
				require.Len(t, content, len(data))
				return &FileInfo{Key: "test-key", Size: size}, nil
			},
		}

		info, err := PutBytes(context.Background(), storage, data, "large.bin")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, int64(len(data)), info.Size)
	})

	t.Run("storage error propagates", func(t *testing.T) {
		t.Parallel()

		storageErr := errors.New("storage unavailable")
		storage := &mockStorage{
			putFunc: func(_ context.Context, _ io.Reader, _ int64, _ ...Option) (*FileInfo, error) {
				return nil, storageErr
			},
		}

		_, err := PutBytes(context.Background(), storage, []byte("data"), "test.txt")
		require.Error(t, err)
		require.True(t, errors.Is(err, storageErr))
	})
}

// TestPutFromURL tests the PutFromURL helper function.
func TestPutFromURL(t *testing.T) {
	t.Parallel()

	t.Run("invalid URL returns ErrInvalidURL", func(t *testing.T) {
		t.Parallel()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, "not-a-valid-url", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrInvalidURL))
	})

	t.Run("non-http scheme returns ErrInvalidURL", func(t *testing.T) {
		t.Parallel()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, "ftp://example.com/file.txt", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrInvalidURL))
	})

	t.Run("file scheme returns ErrInvalidURL", func(t *testing.T) {
		t.Parallel()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, "file:///etc/passwd", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrInvalidURL))
	})

	t.Run("non-200 status returns ErrDownloadFailed", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrDownloadFailed))
		require.Contains(t, err.Error(), "404")
	})

	t.Run("server error returns ErrDownloadFailed", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrDownloadFailed))
		require.Contains(t, err.Error(), "500")
	})

	t.Run("ContentLength exceeds maxSize returns ErrDownloadTooLarge", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1000000")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 1024)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrDownloadTooLarge))
	})

	t.Run("actual download exceeds maxSize returns ErrDownloadTooLarge", func(t *testing.T) {
		t.Parallel()

		// Server doesn't send Content-Length but sends more data than limit
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Write more data than the limit
			data := make([]byte, 2048)
			_, _ = w.Write(data)
		}))
		defer server.Close()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 1024)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrDownloadTooLarge))
	})

	t.Run("empty response returns ErrEmptyFile", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Don't write any data
		}))
		defer server.Close()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrEmptyFile))
	})

	t.Run("successful download and upload", func(t *testing.T) {
		t.Parallel()

		expectedData := []byte("hello from server")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(expectedData)
		}))
		defer server.Close()

		var capturedData []byte
		var capturedSize int64
		storage := &mockStorage{
			putFunc: func(_ context.Context, r io.Reader, size int64, _ ...Option) (*FileInfo, error) {
				capturedSize = size
				var err error
				capturedData, err = io.ReadAll(r)
				require.NoError(t, err)
				return &FileInfo{Key: "test-key", Size: size}, nil
			},
		}

		info, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 0)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, expectedData, capturedData)
		require.Equal(t, int64(len(expectedData)), capturedSize)
	})

	t.Run("uses default maxSize when 0", func(t *testing.T) {
		t.Parallel()

		// Server sends data slightly under default limit
		dataSize := int64(DefaultMaxDownloadSize - 1024)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			data := make([]byte, dataSize)
			_, _ = w.Write(data)
		}))
		defer server.Close()

		storage := &mockStorage{}
		info, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 0)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, dataSize, info.Size)
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()

		// Server that waits before responding
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Second):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("data"))
			}
		}))
		defer server.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		storage := &mockStorage{}
		_, err := PutFromURL(ctx, storage, server.URL+"/file.txt", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrDownloadFailed))
	})

	t.Run("http scheme accepted", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data"))
		}))
		defer server.Close()

		storage := &mockStorage{}
		info, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 0)
		require.NoError(t, err)
		require.NotNil(t, info)
	})

	t.Run("storage error propagates", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data"))
		}))
		defer server.Close()

		storageErr := errors.New("storage unavailable")
		storage := &mockStorage{
			putFunc: func(_ context.Context, _ io.Reader, _ int64, _ ...Option) (*FileInfo, error) {
				return nil, storageErr
			},
		}

		_, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, storageErr))
	})

	t.Run("connection refused returns ErrDownloadFailed", func(t *testing.T) {
		t.Parallel()

		storage := &mockStorage{}
		// Use a port that's unlikely to have anything listening
		_, err := PutFromURL(context.Background(), storage, "http://127.0.0.1:59999/file.txt", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrDownloadFailed))
	})

	t.Run("exact maxSize is allowed", func(t *testing.T) {
		t.Parallel()

		maxSize := int64(100)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			data := make([]byte, maxSize)
			_, _ = w.Write(data)
		}))
		defer server.Close()

		storage := &mockStorage{}
		info, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", maxSize)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, maxSize, info.Size)
	})

	t.Run("one byte over maxSize fails", func(t *testing.T) {
		t.Parallel()

		maxSize := int64(100)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			data := make([]byte, maxSize+1)
			_, _ = w.Write(data)
		}))
		defer server.Close()

		storage := &mockStorage{}
		_, err := PutFromURL(context.Background(), storage, server.URL+"/file.txt", maxSize)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrDownloadTooLarge))
	})
}
