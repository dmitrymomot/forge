package idempotency

import (
	"errors"

	"github.com/dmitrymomot/forge/core/errorsx"
)

// Rejection sentinels. Each carries a stable code surfaced as problem+json Code.
var (
	ErrKeyRequired     = errorsx.New("idempotency_key_required", "idempotency key required")
	ErrInProgress      = errorsx.New("idempotency_in_progress", "a request with this idempotency key is already in progress")
	ErrKeyReuse        = errorsx.New("idempotency_key_reuse", "idempotency key reused with a different request payload")
	ErrRequestTooLarge = errorsx.New("idempotency_request_too_large", "request body exceeds the idempotency size limit")
	ErrReadBody        = errorsx.New("idempotency_read_body", "could not read request body")
)

// ErrCorruptRecord marks a stored record that failed to decode. Treated as an
// in-progress claim rather than a 500, so a poisoned entry cannot wedge a key.
var ErrCorruptRecord = errors.New("idempotency: corrupt stored record")
