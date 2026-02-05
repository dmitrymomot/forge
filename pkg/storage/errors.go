package storage

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Sentinel errors for storage operations.
var (
	// Configuration errors.
	ErrInvalidConfig = errors.New("storage: invalid configuration")

	// File errors.
	ErrEmptyFile = errors.New("storage: file is empty")

	// Validation errors.
	ErrFileTooLarge = errors.New("storage: file exceeds size limit")
	ErrFileTooSmall = errors.New("storage: file below minimum size")
	ErrInvalidMIME  = errors.New("storage: file type not allowed")

	// S3 operation errors.
	ErrNotFound      = errors.New("storage: file not found")
	ErrAccessDenied  = errors.New("storage: access denied")
	ErrUploadFailed  = errors.New("storage: upload failed")
	ErrDeleteFailed  = errors.New("storage: delete failed")
	ErrPresignFailed = errors.New("storage: presign failed")

	// URL errors.
	ErrInvalidURL       = errors.New("storage: invalid URL")
	ErrDownloadFailed   = errors.New("storage: failed to download from URL")
	ErrDownloadTooLarge = errors.New("storage: download exceeds size limit")
)

// wrapS3Error wraps S3 errors with appropriate sentinel errors.
// It checks both API error codes and typed errors for comprehensive error handling.
// Note: Uses %v (not %w) for the original error to normalize error types -
// callers should use errors.Is() with sentinel errors, not errors.As() for AWS types.
func wrapS3Error(err error, fallback error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		case "AccessDenied", "Forbidden":
			return fmt.Errorf("%w: %v", ErrAccessDenied, err)
		}
	}

	// Check for S3 typed errors.
	var notFound *types.NoSuchKey
	if errors.As(err, &notFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}

	return fmt.Errorf("%w: %v", fallback, err)
}
