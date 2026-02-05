package storage

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/require"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	// Verify all sentinel errors are distinct.
	sentinels := []error{
		ErrInvalidConfig,
		ErrEmptyFile,
		ErrFileTooLarge,
		ErrFileTooSmall,
		ErrInvalidMIME,
		ErrNotFound,
		ErrAccessDenied,
		ErrUploadFailed,
		ErrDeleteFailed,
		ErrPresignFailed,
		ErrInvalidURL,
		ErrDownloadFailed,
		ErrDownloadTooLarge,
	}

	// Check each error is unique.
	seen := make(map[string]bool)
	for _, err := range sentinels {
		msg := err.Error()
		require.False(t, seen[msg], "duplicate error message: %s", msg)
		seen[msg] = true
	}
}

// mockAPIError implements smithy.APIError for testing.
type mockAPIError struct {
	code    string
	message string
}

func (e *mockAPIError) ErrorCode() string             { return e.code }
func (e *mockAPIError) ErrorMessage() string          { return e.message }
func (e *mockAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }
func (e *mockAPIError) Error() string                 { return fmt.Sprintf("%s: %s", e.code, e.message) }

func TestWrapS3Error(t *testing.T) {
	t.Parallel()

	t.Run("NoSuchKey code", func(t *testing.T) {
		t.Parallel()
		apiErr := &mockAPIError{code: "NoSuchKey", message: "key not found"}
		wrapped := wrapS3Error(apiErr, ErrUploadFailed)
		require.ErrorIs(t, wrapped, ErrNotFound)
	})

	t.Run("NotFound code", func(t *testing.T) {
		t.Parallel()
		apiErr := &mockAPIError{code: "NotFound", message: "not found"}
		wrapped := wrapS3Error(apiErr, ErrUploadFailed)
		require.ErrorIs(t, wrapped, ErrNotFound)
	})

	t.Run("AccessDenied code", func(t *testing.T) {
		t.Parallel()
		apiErr := &mockAPIError{code: "AccessDenied", message: "access denied"}
		wrapped := wrapS3Error(apiErr, ErrUploadFailed)
		require.ErrorIs(t, wrapped, ErrAccessDenied)
	})

	t.Run("Forbidden code", func(t *testing.T) {
		t.Parallel()
		apiErr := &mockAPIError{code: "Forbidden", message: "forbidden"}
		wrapped := wrapS3Error(apiErr, ErrUploadFailed)
		require.ErrorIs(t, wrapped, ErrAccessDenied)
	})

	t.Run("NoSuchKey typed error", func(t *testing.T) {
		t.Parallel()
		typedErr := &types.NoSuchKey{}
		wrapped := wrapS3Error(typedErr, ErrUploadFailed)
		require.ErrorIs(t, wrapped, ErrNotFound)
	})

	t.Run("fallback error", func(t *testing.T) {
		t.Parallel()
		plainErr := errors.New("some error")
		wrapped := wrapS3Error(plainErr, ErrUploadFailed)
		require.ErrorIs(t, wrapped, ErrUploadFailed)
	})

	t.Run("unknown API error code", func(t *testing.T) {
		t.Parallel()
		apiErr := &mockAPIError{code: "UnknownError", message: "unknown"}
		wrapped := wrapS3Error(apiErr, ErrDeleteFailed)
		require.ErrorIs(t, wrapped, ErrDeleteFailed)
	})
}
