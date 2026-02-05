//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/storage"
)

// Integration test configuration for rustfs (S3-compatible storage).
// Start the test infrastructure with: docker-compose up -d
const (
	testEndpoint  = "http://localhost:9000"
	testAccessKey = "admin"
	testSecretKey = "admin123"
	testBucket    = "uploads"
	testRegion    = "us-east-1"
)

func newTestStorage(t *testing.T) *storage.S3Storage {
	t.Helper()

	s, err := storage.New(storage.Config{
		Endpoint:  testEndpoint,
		AccessKey: testAccessKey,
		SecretKey: testSecretKey,
		Bucket:    testBucket,
		Region:    testRegion,
		PathStyle: true,
	})
	require.NoError(t, err, "failed to create storage client")

	return s
}

func TestS3Integration_Put(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := context.Background()

	t.Run("upload with private ACL", func(t *testing.T) {
		t.Parallel()

		data := []byte("test content for private file")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithPrefix("test-private"),
			storage.WithACL(storage.ACLPrivate),
		)
		require.NoError(t, err)
		require.NotEmpty(t, info.Key)
		require.Equal(t, int64(len(data)), info.Size)
		require.Equal(t, storage.ACLPrivate, info.ACL)

		// Cleanup
		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})
	})

	t.Run("upload with public-read ACL", func(t *testing.T) {
		t.Parallel()

		data := []byte("test content for public file")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithPrefix("test-public"),
			storage.WithACL(storage.ACLPublicRead),
		)
		require.NoError(t, err)
		require.NotEmpty(t, info.Key)
		require.Equal(t, int64(len(data)), info.Size)
		require.Equal(t, storage.ACLPublicRead, info.ACL)

		// Cleanup
		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})
	})

	t.Run("upload with tenant prefix", func(t *testing.T) {
		t.Parallel()

		data := []byte("test content with tenant")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithTenant("tenant123"),
			storage.WithPrefix("uploads"),
		)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(info.Key, "tenant123/"))

		// Cleanup
		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})
	})

	t.Run("upload detects MIME type", func(t *testing.T) {
		t.Parallel()

		// PNG magic bytes
		pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		pngData = append(pngData, make([]byte, 100)...)

		info, err := s.Put(ctx, bytes.NewReader(pngData), int64(len(pngData)))
		require.NoError(t, err)
		require.Equal(t, "image/png", info.ContentType)
		require.True(t, strings.HasSuffix(info.Key, ".png"))

		// Cleanup
		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})
	})

	t.Run("upload with explicit content type", func(t *testing.T) {
		t.Parallel()

		data := []byte("some binary data")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithContentType("application/octet-stream"),
		)
		require.NoError(t, err)
		require.Equal(t, "application/octet-stream", info.ContentType)

		// Cleanup
		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})
	})
}

func TestS3Integration_Get(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := context.Background()

	t.Run("retrieve uploaded file", func(t *testing.T) {
		t.Parallel()

		expectedData := []byte("content to retrieve")
		info, err := s.Put(ctx, bytes.NewReader(expectedData), int64(len(expectedData)))
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})

		reader, err := s.Get(ctx, info.Key)
		require.NoError(t, err)
		defer reader.Close()

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, expectedData, data)
	})

	t.Run("get non-existent file returns error", func(t *testing.T) {
		t.Parallel()

		_, err := s.Get(ctx, "non-existent-key-12345")
		require.Error(t, err)
		require.ErrorIs(t, err, storage.ErrNotFound)
	})
}

func TestS3Integration_Delete(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := context.Background()

	t.Run("delete existing file", func(t *testing.T) {
		t.Parallel()

		data := []byte("content to delete")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		err = s.Delete(ctx, info.Key)
		require.NoError(t, err)

		// Verify file is gone
		_, err = s.Get(ctx, info.Key)
		require.Error(t, err)
		require.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("delete non-existent file succeeds", func(t *testing.T) {
		t.Parallel()

		// S3 delete is idempotent
		err := s.Delete(ctx, "non-existent-key-67890")
		require.NoError(t, err)
	})
}

func TestS3Integration_URL(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := context.Background()

	t.Run("signed URL for private file", func(t *testing.T) {
		t.Parallel()

		data := []byte("private content")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithACL(storage.ACLPrivate),
		)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})

		url, err := s.URL(ctx, info.Key)
		require.NoError(t, err)
		require.Contains(t, url, info.Key)
		require.Contains(t, url, "X-Amz-Signature") // Signed URL contains signature
	})

	t.Run("public URL for public file", func(t *testing.T) {
		t.Parallel()

		data := []byte("public content")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithACL(storage.ACLPublicRead),
		)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})

		url, err := s.URL(ctx, info.Key)
		require.NoError(t, err)
		require.Contains(t, url, info.Key)
		require.NotContains(t, url, "X-Amz-Signature") // Public URL has no signature
	})

	t.Run("force signed URL for public file", func(t *testing.T) {
		t.Parallel()

		data := []byte("public content with signed url")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithACL(storage.ACLPublicRead),
		)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})

		url, err := s.URL(ctx, info.Key, storage.WithSigned(0))
		require.NoError(t, err)
		require.Contains(t, url, "X-Amz-Signature")
	})

	t.Run("URL with custom expiry", func(t *testing.T) {
		t.Parallel()

		data := []byte("content with custom expiry")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})

		url, err := s.URL(ctx, info.Key, storage.WithExpiry(1*time.Hour))
		require.NoError(t, err)
		require.NotEmpty(t, url)
	})

	t.Run("URL with download disposition", func(t *testing.T) {
		t.Parallel()

		data := []byte("downloadable content")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})

		url, err := s.URL(ctx, info.Key, storage.WithDownload("myfile.txt"))
		require.NoError(t, err)
		require.Contains(t, url, "response-content-disposition")
	})
}

func TestS3Integration_HeadObject(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := context.Background()

	t.Run("get metadata for existing file", func(t *testing.T) {
		t.Parallel()

		data := []byte("content for head request")
		info, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithACL(storage.ACLPublicRead),
		)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, info.Key)
		})

		headInfo, err := s.HeadObject(ctx, info.Key)
		require.NoError(t, err)
		require.Equal(t, info.Key, headInfo.Key)
		require.Equal(t, info.Size, headInfo.Size)
		require.Equal(t, info.ContentType, headInfo.ContentType)
		require.Equal(t, storage.ACLPublicRead, headInfo.ACL)
	})

	t.Run("head non-existent file returns error", func(t *testing.T) {
		t.Parallel()

		_, err := s.HeadObject(ctx, "non-existent-key-head")
		require.Error(t, err)
		require.ErrorIs(t, err, storage.ErrNotFound)
	})
}

func TestS3Integration_Copy(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := context.Background()

	t.Run("copy file within bucket", func(t *testing.T) {
		t.Parallel()

		data := []byte("content to copy")
		srcInfo, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithPrefix("source"),
		)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, srcInfo.Key)
		})

		dstKey := "copied/" + srcInfo.Key
		err = s.Copy(ctx, srcInfo.Key, dstKey)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, dstKey)
		})

		// Verify copy exists with same content
		reader, err := s.Get(ctx, dstKey)
		require.NoError(t, err)
		defer reader.Close()

		copiedData, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, data, copiedData)
	})

	t.Run("copy preserves ACL", func(t *testing.T) {
		t.Parallel()

		data := []byte("public content to copy")
		srcInfo, err := s.Put(ctx, bytes.NewReader(data), int64(len(data)),
			storage.WithPrefix("source-public"),
			storage.WithACL(storage.ACLPublicRead),
		)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, srcInfo.Key)
		})

		dstKey := "copied-public/" + srcInfo.Key
		err = s.Copy(ctx, srcInfo.Key, dstKey)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = s.Delete(ctx, dstKey)
		})

		// Get URL for copied file - should be public (no signature)
		url, err := s.URL(ctx, dstKey)
		require.NoError(t, err)
		require.NotContains(t, url, "X-Amz-Signature")
	})

	t.Run("copy non-existent source returns error", func(t *testing.T) {
		t.Parallel()

		err := s.Copy(ctx, "non-existent-source", "destination-key")
		require.Error(t, err)
	})
}
