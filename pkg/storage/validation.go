package storage

import (
	"fmt"
	"mime/multipart"
)

// FileValidationError represents a file validation failure.
type FileValidationError struct {
	Details map[string]any // Error-specific data
	Field   string         // Form field name (e.g., "file")
	Code    string         // Error code (e.g., "file_too_large", "invalid_mime", "empty_file")
	Message string         // Human-readable message
}

// Error implements the error interface.
func (e *FileValidationError) Error() string {
	return e.Message
}

// Error codes for FileValidationError.
const (
	ErrCodeFileTooLarge = "file_too_large"
	ErrCodeFileTooSmall = "file_too_small"
	ErrCodeInvalidMIME  = "invalid_mime"
	ErrCodeEmptyFile    = "empty_file"
)

// ValidationRule defines a validation check for file uploads.
type ValidationRule interface {
	// Validate checks the file and returns an error if validation fails.
	Validate(fh *multipart.FileHeader, mimeType string) error
}

// ValidateFile runs all validation rules against a file.
// Returns the first validation error encountered, or nil if all pass.
// The mimeType should be pre-detected from magic bytes for accuracy.
func ValidateFile(fh *multipart.FileHeader, mimeType string, rules ...ValidationRule) error {
	for _, rule := range rules {
		if err := rule.Validate(fh, mimeType); err != nil {
			return err
		}
	}
	return nil
}

// maxSizeRule validates that file size is within limits.
type maxSizeRule struct {
	maxBytes int64
}

// MaxSize returns a rule that rejects files larger than the specified size.
func MaxSize(bytes int64) ValidationRule {
	return &maxSizeRule{maxBytes: bytes}
}

// Validate implements ValidationRule.
func (r *maxSizeRule) Validate(fh *multipart.FileHeader, _ string) error {
	if fh.Size > r.maxBytes {
		return &FileValidationError{
			Field:   "file",
			Code:    ErrCodeFileTooLarge,
			Message: fmt.Sprintf("file size %d exceeds limit of %d bytes", fh.Size, r.maxBytes),
			Details: map[string]any{
				"limit": r.maxBytes,
				"got":   fh.Size,
			},
		}
	}
	return nil
}

// minSizeRule validates that file size meets minimum.
type minSizeRule struct {
	minBytes int64
}

// MinSize returns a rule that rejects files smaller than the specified size.
func MinSize(bytes int64) ValidationRule {
	return &minSizeRule{minBytes: bytes}
}

// Validate implements ValidationRule.
func (r *minSizeRule) Validate(fh *multipart.FileHeader, _ string) error {
	if fh.Size < r.minBytes {
		return &FileValidationError{
			Field:   "file",
			Code:    ErrCodeFileTooSmall,
			Message: fmt.Sprintf("file size %d is below minimum of %d bytes", fh.Size, r.minBytes),
			Details: map[string]any{
				"minimum": r.minBytes,
				"got":     fh.Size,
			},
		}
	}
	return nil
}

// notEmptyRule validates that the file is not empty.
type notEmptyRule struct{}

// NotEmpty returns a rule that rejects empty files.
func NotEmpty() ValidationRule {
	return &notEmptyRule{}
}

// Validate implements ValidationRule.
func (r *notEmptyRule) Validate(fh *multipart.FileHeader, _ string) error {
	if fh == nil || fh.Size == 0 {
		return &FileValidationError{
			Field:   "file",
			Code:    ErrCodeEmptyFile,
			Message: "file is empty",
			Details: map[string]any{},
		}
	}
	return nil
}

// allowedTypesRule validates MIME type against allowed patterns.
type allowedTypesRule struct {
	patterns []string
}

// AllowedTypes returns a rule that only accepts files matching the given MIME patterns.
// Supports wildcards like "image/*".
func AllowedTypes(patterns ...string) ValidationRule {
	return &allowedTypesRule{patterns: patterns}
}

// Validate implements ValidationRule.
func (r *allowedTypesRule) Validate(_ *multipart.FileHeader, mimeType string) error {
	if !matchesMIME(mimeType, r.patterns) {
		return &FileValidationError{
			Field:   "file",
			Code:    ErrCodeInvalidMIME,
			Message: fmt.Sprintf("file type %q is not allowed", mimeType),
			Details: map[string]any{
				"type":    mimeType,
				"allowed": r.patterns,
			},
		}
	}
	return nil
}

// ImageOnly returns a rule that only accepts image files.
// Equivalent to AllowedTypes("image/*").
func ImageOnly() ValidationRule {
	return AllowedTypes("image/*")
}

// DocumentsOnly returns a rule that only accepts document files.
// Includes PDF, Word, Excel, PowerPoint, text, and CSV files.
func DocumentsOnly() ValidationRule {
	return AllowedTypes(
		"application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"text/plain",
		"text/csv",
		"application/rtf",
	)
}
