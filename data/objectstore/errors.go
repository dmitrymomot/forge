package objectstore

import "errors"

var (
	// ErrNotFound reports a key with no stored object. Get and Stat wrap it
	// with the key; match with errors.Is.
	ErrNotFound = errors.New("objectstore: not found")

	// ErrInvalidKey reports a key that fails validation: empty, longer than
	// 1024 bytes, not UTF-8, containing control characters or backslashes, or
	// with empty, ".", or ".." path segments.
	ErrInvalidKey = errors.New("objectstore: invalid key")

	// ErrTooLarge reports content that exceeded the WithMaxSize cap during
	// Put. The backend write is aborted; no object is stored.
	ErrTooLarge = errors.New("objectstore: content exceeds size limit")

	// ErrUnsupportedType reports content whose magic-byte-detected MIME is
	// not in the WithAllowedTypes allowlist (or could not be detected at all).
	ErrUnsupportedType = errors.New("objectstore: unsupported content type")

	// ErrNotSupported reports a capability the configured Store lacks, e.g.
	// signed URLs on a backend that does not implement URLSigner.
	ErrNotSupported = errors.New("objectstore: operation not supported by store")

	// ErrScope reports a failed tenant-scope resolution: the WithScope hook
	// returned an error, an empty scope, or an invalid scope value. Scoped
	// buckets fail closed — no operation reaches the backend.
	ErrScope = errors.New("objectstore: tenant scope unavailable")

	// ErrNilStore reports New called with a nil Store.
	ErrNilStore = errors.New("objectstore: nil store")
)
