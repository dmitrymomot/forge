package oauth

import "errors"

var (
	// ErrMissingClientID is returned when the OAuth client ID is not provided.
	ErrMissingClientID = errors.New("oauth: missing client ID")

	// ErrMissingClientSecret is returned when the OAuth client secret is not provided.
	ErrMissingClientSecret = errors.New("oauth: missing client secret")

	// ErrEmailNotVerified is returned when the OAuth provider reports
	// that the user's email is not verified.
	ErrEmailNotVerified = errors.New("oauth: email not verified")

	// ErrNilResponse is returned when the OAuth provider returns a nil response.
	ErrNilResponse = errors.New("oauth: nil response from provider")

	// ErrFetchFailed is returned when fetching data from the OAuth provider fails.
	ErrFetchFailed = errors.New("oauth: failed to fetch from provider")

	// ErrRequestFailed is returned when the OAuth provider returns a non-OK status.
	ErrRequestFailed = errors.New("oauth: request returned non-OK status")

	// ErrDecodeFailed is returned when decoding the OAuth provider response fails.
	ErrDecodeFailed = errors.New("oauth: failed to decode response")
)
