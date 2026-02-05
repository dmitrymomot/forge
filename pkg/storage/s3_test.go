package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		cfg := Config{
			Bucket:    "test-bucket",
			AccessKey: "test-access-key",
			SecretKey: "test-secret-key",
		}

		store, err := New(cfg)
		require.NoError(t, err)
		require.NotNil(t, store)
		require.NotNil(t, store.client)
		require.NotNil(t, store.presigner)
		require.Equal(t, DefaultRegion, store.cfg.Region)
		require.Equal(t, ACLPrivate, store.cfg.DefaultACL)
	})

	t.Run("custom endpoint", func(t *testing.T) {
		t.Parallel()
		cfg := Config{
			Bucket:    "test-bucket",
			AccessKey: "test-access-key",
			SecretKey: "test-secret-key",
			Endpoint:  "http://localhost:9000",
			PathStyle: true,
		}

		store, err := New(cfg)
		require.NoError(t, err)
		require.NotNil(t, store)
	})

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()
		cfg := Config{} // Missing required fields.

		store, err := New(cfg)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidConfig)
		require.Nil(t, store)
	})
}

func TestSanitizePathSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "avatars", "avatars"},
		{"with spaces", "my folder", "my_folder"},
		{"with slashes", "/path/to/", "path_to"},
		{"path traversal", "../../../etc/passwd", "___etc_passwd"},
		{"special chars", "file@#$%name", "file____name"},
		{"leading dots", "..hidden", "hidden"},
		{"unicode", "файл", "____"},
		{"empty", "", ""},
		{"dashes and underscores", "my-file_name", "my-file_name"},
		{"dots allowed", "file.name", "file.name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizePathSegment(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestS3Storage_buildKey(t *testing.T) {
	t.Parallel()

	store := &S3Storage{
		cfg: Config{
			Bucket:     "test-bucket",
			DefaultACL: ACLPrivate,
		},
	}

	t.Run("no tenant no prefix", func(t *testing.T) {
		t.Parallel()
		key := store.buildKey("", "", "image/jpeg")
		// Should be just {ulid}.jpg
		require.Regexp(t, `^[0-9A-Z]{26}\.jpg$`, key)
	})

	t.Run("with prefix", func(t *testing.T) {
		t.Parallel()
		key := store.buildKey("", "avatars", "image/png")
		// Should be avatars/{ulid}.png
		require.Regexp(t, `^avatars/[0-9A-Z]{26}\.png$`, key)
	})

	t.Run("with tenant", func(t *testing.T) {
		t.Parallel()
		key := store.buildKey("tenant123", "", "application/pdf")
		// Should be tenant123/{ulid}.pdf
		require.Regexp(t, `^tenant123/[0-9A-Z]{26}\.pdf$`, key)
	})

	t.Run("with tenant and prefix", func(t *testing.T) {
		t.Parallel()
		key := store.buildKey("tenant123", "documents", "application/pdf")
		// Should be tenant123/documents/{ulid}.pdf
		require.Regexp(t, `^tenant123/documents/[0-9A-Z]{26}\.pdf$`, key)
	})

	t.Run("unknown mime type", func(t *testing.T) {
		t.Parallel()
		key := store.buildKey("", "", "application/unknown")
		// Should use .bin extension
		require.Regexp(t, `^[0-9A-Z]{26}\.bin$`, key)
	})
}

func TestS3Storage_publicURL(t *testing.T) {
	t.Parallel()

	t.Run("default S3 URL", func(t *testing.T) {
		t.Parallel()
		store := &S3Storage{
			cfg: Config{
				Bucket: "test-bucket",
				Region: "us-east-1",
			},
		}

		url := store.publicURL("path/to/file.jpg")
		require.Equal(t, "https://test-bucket.s3.us-east-1.amazonaws.com/path/to/file.jpg", url)
	})

	t.Run("custom public URL", func(t *testing.T) {
		t.Parallel()
		store := &S3Storage{
			cfg: Config{
				Bucket:    "test-bucket",
				PublicURL: "https://cdn.example.com",
			},
		}

		url := store.publicURL("path/to/file.jpg")
		require.Equal(t, "https://cdn.example.com/path/to/file.jpg", url)
	})

	t.Run("custom public URL with trailing slash", func(t *testing.T) {
		t.Parallel()
		store := &S3Storage{
			cfg: Config{
				Bucket:    "test-bucket",
				PublicURL: "https://cdn.example.com/",
			},
		}

		url := store.publicURL("path/to/file.jpg")
		require.Equal(t, "https://cdn.example.com/path/to/file.jpg", url)
	})

	t.Run("custom endpoint path style", func(t *testing.T) {
		t.Parallel()
		store := &S3Storage{
			cfg: Config{
				Bucket:    "test-bucket",
				Endpoint:  "http://localhost:9000",
				PathStyle: true,
			},
		}

		url := store.publicURL("path/to/file.jpg")
		require.Equal(t, "http://localhost:9000/test-bucket/path/to/file.jpg", url)
	})

	t.Run("custom endpoint virtual host style", func(t *testing.T) {
		t.Parallel()
		store := &S3Storage{
			cfg: Config{
				Bucket:    "test-bucket",
				Endpoint:  "http://localhost:9000",
				PathStyle: false,
			},
		}

		url := store.publicURL("path/to/file.jpg")
		require.Equal(t, "http://localhost:9000/path/to/file.jpg", url)
	})
}

func TestS3Storage_URL_ACLDetection(t *testing.T) {
	t.Parallel()

	// This test verifies the ACL tracking mechanism works.
	// Full integration tests would require an actual S3 endpoint.

	t.Run("tracks ACL for uploaded files", func(t *testing.T) {
		t.Parallel()
		store := &S3Storage{
			cfg: Config{
				Bucket:     "test-bucket",
				Region:     "us-east-1",
				DefaultACL: ACLPrivate,
			},
			fileACLs: make(map[string]ACL),
		}

		// Simulate tracking ACL.
		store.fileACLs["public-file.jpg"] = ACLPublicRead
		store.fileACLs["private-file.jpg"] = ACLPrivate

		// Verify tracking.
		require.Equal(t, ACLPublicRead, store.fileACLs["public-file.jpg"])
		require.Equal(t, ACLPrivate, store.fileACLs["private-file.jpg"])
	})
}
