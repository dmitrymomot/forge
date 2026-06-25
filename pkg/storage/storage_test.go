package storage

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_applyDefaults(t *testing.T) {
	t.Parallel()

	t.Run("empty config gets defaults", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		cfg.applyDefaults()

		require.Equal(t, DefaultRegion, cfg.Region)
		require.Equal(t, string(ACLPrivate), cfg.DefaultACL)
		require.Equal(t, int64(DefaultMaxDownloadSize), cfg.MaxDownloadSize)
	})

	t.Run("existing values preserved", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Region:          "eu-west-1",
			DefaultACL:      string(ACLPublicRead),
			MaxDownloadSize: 100 << 20,
		}
		cfg.applyDefaults()

		require.Equal(t, "eu-west-1", cfg.Region)
		require.Equal(t, string(ACLPublicRead), cfg.DefaultACL)
		require.Equal(t, int64(100<<20), cfg.MaxDownloadSize)
	})
}

func TestConfig_validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Bucket:    "my-bucket",
				AccessKey: "access-key",
				SecretKey: "secret-key",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			cfg: Config{
				AccessKey: "access-key",
				SecretKey: "secret-key",
			},
			wantErr: true,
		},
		{
			name: "missing access key",
			cfg: Config{
				Bucket:    "my-bucket",
				SecretKey: "secret-key",
			},
			wantErr: true,
		},
		{
			name: "missing secret key",
			cfg: Config{
				Bucket:    "my-bucket",
				AccessKey: "access-key",
			},
			wantErr: true,
		},
		{
			name:    "empty config",
			cfg:     Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidConfig)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSanitizePathPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{"empty", "", ""},
		{"single segment", "avatars", "avatars"},
		{"multi segment preserved", "users/avatars", "users/avatars"},
		{"deep multi segment", "a/b/c/d", "a/b/c/d"},
		{"leading and trailing slashes trimmed", "/users/avatars/", "users/avatars"},
		{"backslash separators", "users\\avatars", "users/avatars"},
		{"empty segments dropped", "users//avatars", "users/avatars"},
		{"traversal stripped per segment", "users/../../etc", "users/etc"},
		{"unsafe chars replaced", "a b/c@d", "a_b/c_d"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, sanitizePathPrefix(tc.prefix))
		})
	}
}

func TestBuildKey_MultiSegmentPrefix(t *testing.T) {
	t.Parallel()

	s := &S3Storage{}

	t.Run("preserves multi-segment prefix", func(t *testing.T) {
		t.Parallel()
		key := s.buildKey("tenant1", "users/avatars", "image/png", "")
		require.True(t, strings.HasPrefix(key, "tenant1/users/avatars/"),
			"key %q should keep all prefix segments", key)
		require.True(t, strings.HasSuffix(key, ".png"))
		// tenant + 2 prefix segments + filename = 4 path components.
		require.Len(t, strings.Split(key, "/"), 4)
	})

	t.Run("filename ext used when MIME unknown", func(t *testing.T) {
		t.Parallel()
		key := s.buildKey("", "", "application/octet-stream", ".pdf")
		require.True(t, strings.HasSuffix(key, ".pdf"), "got %q", key)
	})

	t.Run("MIME extension wins over filename hint", func(t *testing.T) {
		t.Parallel()
		key := s.buildKey("", "", "image/png", ".pdf")
		require.True(t, strings.HasSuffix(key, ".png"), "got %q", key)
	})

	t.Run("falls back to .bin without MIME or hint", func(t *testing.T) {
		t.Parallel()
		key := s.buildKey("", "", "application/octet-stream", "")
		require.True(t, strings.HasSuffix(key, ".bin"), "got %q", key)
	})
}

func TestMaxBytesFromRules(t *testing.T) {
	t.Parallel()

	t.Run("no MaxSize rule yields -1", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, int64(-1), maxBytesFromRules([]ValidationRule{NotEmpty(), ImageOnly()}))
	})

	t.Run("returns the single MaxSize limit", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, int64(1024), maxBytesFromRules([]ValidationRule{MaxSize(1024)}))
	})

	t.Run("returns the smallest of multiple MaxSize limits", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, int64(512), maxBytesFromRules([]ValidationRule{MaxSize(1024), MaxSize(512)}))
	})
}

func TestReadLimited(t *testing.T) {
	t.Parallel()

	t.Run("no limit reads everything", func(t *testing.T) {
		t.Parallel()
		data, err := readLimited(strings.NewReader("hello"), -1)
		require.NoError(t, err)
		require.Equal(t, []byte("hello"), data)
	})

	t.Run("within limit succeeds", func(t *testing.T) {
		t.Parallel()
		data, err := readLimited(strings.NewReader("hello"), 5)
		require.NoError(t, err)
		require.Equal(t, []byte("hello"), data)
	})

	t.Run("over limit returns FileValidationError without reading it all", func(t *testing.T) {
		t.Parallel()

		// A reader that counts how many bytes were consumed; if the limit were
		// not enforced during streaming, all 1MB would be read.
		const total = 1 << 20
		cr := &countingReader{r: io.LimitReader(zeroReader{}, total)}

		_, err := readLimited(cr, 100)
		require.Error(t, err)

		var verr *FileValidationError
		require.ErrorAs(t, err, &verr)
		require.Equal(t, ErrCodeFileTooLarge, verr.Code)
		// errors.Is via the Unwrap mapping.
		require.True(t, errors.Is(err, ErrFileTooLarge))

		// Only maxBytes+1 bytes should have been consumed, far less than total.
		require.LessOrEqual(t, cr.n, int64(101))
	})
}

func TestFileValidationError_Unwrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code     string
		sentinel error
	}{
		{ErrCodeFileTooLarge, ErrFileTooLarge},
		{ErrCodeFileTooSmall, ErrFileTooSmall},
		{ErrCodeInvalidMIME, ErrInvalidMIME},
		{ErrCodeEmptyFile, ErrEmptyFile},
	}

	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			err := error(&FileValidationError{Code: tc.code, Message: "x"})
			require.True(t, errors.Is(err, tc.sentinel),
				"errors.Is should match sentinel for code %q", tc.code)
		})
	}

	t.Run("unknown code unwraps to nil", func(t *testing.T) {
		t.Parallel()
		verr := &FileValidationError{Code: "something_else"}
		require.NoError(t, verr.Unwrap())
	})
}

// zeroReader yields an endless stream of zero bytes.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// countingReader records how many bytes were read from the wrapped reader.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
