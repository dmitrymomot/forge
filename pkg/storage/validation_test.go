package storage

import (
	"errors"
	"mime/multipart"
	"net/textproto"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockFileHeader creates a mock multipart.FileHeader for testing.
func mockFileHeader(filename string, size int64) *multipart.FileHeader {
	return &multipart.FileHeader{
		Filename: filename,
		Size:     size,
		Header:   textproto.MIMEHeader{},
	}
}

func TestMaxSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		maxBytes  int64
		fileSize  int64
		wantError bool
		wantCode  string
	}{
		{"under limit", 1024, 512, false, ""},
		{"at limit", 1024, 1024, false, ""},
		{"over limit", 1024, 2048, true, ErrCodeFileTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := MaxSize(tt.maxBytes)
			fh := mockFileHeader("test.txt", tt.fileSize)

			err := rule.Validate(fh, "text/plain")

			if tt.wantError {
				require.Error(t, err)
				var verr *FileValidationError
				require.True(t, errors.As(err, &verr))
				require.Equal(t, tt.wantCode, verr.Code)
				require.Contains(t, verr.Details, "limit")
				require.Contains(t, verr.Details, "got")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMinSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		minBytes  int64
		fileSize  int64
		wantError bool
		wantCode  string
	}{
		{"above minimum", 100, 512, false, ""},
		{"at minimum", 100, 100, false, ""},
		{"below minimum", 100, 50, true, ErrCodeFileTooSmall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := MinSize(tt.minBytes)
			fh := mockFileHeader("test.txt", tt.fileSize)

			err := rule.Validate(fh, "text/plain")

			if tt.wantError {
				require.Error(t, err)
				var verr *FileValidationError
				require.True(t, errors.As(err, &verr))
				require.Equal(t, tt.wantCode, verr.Code)
				require.Contains(t, verr.Details, "minimum")
				require.Contains(t, verr.Details, "got")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNotEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fh        *multipart.FileHeader
		wantError bool
	}{
		{"valid file", mockFileHeader("test.txt", 100), false},
		{"empty file", mockFileHeader("test.txt", 0), true},
		{"nil file", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := NotEmpty()
			err := rule.Validate(tt.fh, "text/plain")

			if tt.wantError {
				require.Error(t, err)
				var verr *FileValidationError
				require.True(t, errors.As(err, &verr))
				require.Equal(t, ErrCodeEmptyFile, verr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAllowedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		patterns  []string
		mimeType  string
		wantError bool
	}{
		{"exact match", []string{"image/jpeg"}, "image/jpeg", false},
		{"wildcard match", []string{"image/*"}, "image/png", false},
		{"multiple patterns", []string{"image/*", "application/pdf"}, "application/pdf", false},
		{"no match", []string{"image/*"}, "video/mp4", true},
		{"case insensitive", []string{"image/jpeg"}, "IMAGE/JPEG", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := AllowedTypes(tt.patterns...)
			fh := mockFileHeader("test.file", 100)

			err := rule.Validate(fh, tt.mimeType)

			if tt.wantError {
				require.Error(t, err)
				var verr *FileValidationError
				require.True(t, errors.As(err, &verr))
				require.Equal(t, ErrCodeInvalidMIME, verr.Code)
				require.Contains(t, verr.Details, "type")
				require.Contains(t, verr.Details, "allowed")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestImageOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mimeType  string
		wantError bool
	}{
		{"jpeg", "image/jpeg", false},
		{"png", "image/png", false},
		{"gif", "image/gif", false},
		{"webp", "image/webp", false},
		{"pdf", "application/pdf", true},
		{"video", "video/mp4", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := ImageOnly()
			fh := mockFileHeader("test.file", 100)

			err := rule.Validate(fh, tt.mimeType)

			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDocumentsOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mimeType  string
		wantError bool
	}{
		{"pdf", "application/pdf", false},
		{"word", "application/msword", false},
		{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", false},
		{"text", "text/plain", false},
		{"csv", "text/csv", false},
		{"image", "image/jpeg", true},
		{"video", "video/mp4", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := DocumentsOnly()
			fh := mockFileHeader("test.file", 100)

			err := rule.Validate(fh, tt.mimeType)

			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateFile(t *testing.T) {
	t.Parallel()

	t.Run("all rules pass", func(t *testing.T) {
		t.Parallel()

		fh := mockFileHeader("test.jpg", 1024)
		err := ValidateFile(fh, "image/jpeg",
			NotEmpty(),
			MaxSize(5<<20),
			ImageOnly(),
		)
		require.NoError(t, err)
	})

	t.Run("first rule fails", func(t *testing.T) {
		t.Parallel()

		fh := mockFileHeader("test.jpg", 0)
		err := ValidateFile(fh, "image/jpeg",
			NotEmpty(),
			MaxSize(5<<20),
		)
		require.Error(t, err)
		var verr *FileValidationError
		require.True(t, errors.As(err, &verr))
		require.Equal(t, ErrCodeEmptyFile, verr.Code)
	})

	t.Run("second rule fails", func(t *testing.T) {
		t.Parallel()

		fh := mockFileHeader("test.jpg", 10<<20)
		err := ValidateFile(fh, "image/jpeg",
			NotEmpty(),
			MaxSize(5<<20),
		)
		require.Error(t, err)
		var verr *FileValidationError
		require.True(t, errors.As(err, &verr))
		require.Equal(t, ErrCodeFileTooLarge, verr.Code)
	})

	t.Run("no rules", func(t *testing.T) {
		t.Parallel()

		fh := mockFileHeader("test.jpg", 1024)
		err := ValidateFile(fh, "image/jpeg")
		require.NoError(t, err)
	})
}

func TestFileValidationError_Error(t *testing.T) {
	t.Parallel()

	err := &FileValidationError{
		Field:   "avatar",
		Code:    ErrCodeFileTooLarge,
		Message: "file size exceeds limit",
		Details: map[string]any{"limit": 5 << 20},
	}

	require.Equal(t, "file size exceeds limit", err.Error())
}
