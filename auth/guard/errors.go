package guard

import (
	"errors"

	"github.com/dmitrymomot/forge/core/errorsx"
)

// ErrNoCredential is passed to the responder when no extractor found a
// credential on the request (or the Basic Auth header is missing/malformed).
var ErrNoCredential = errorsx.New("auth_missing", "no credential provided")

// ErrInvalidCredential is passed to the responder when a credential was
// present but rejected — the verifier returned an error (wrapped, so both
// this sentinel and the verifier's own error match via errors.Is), the
// verifier resolved an Identity without a Subject, or Basic Auth
// credentials did not match.
var ErrInvalidCredential = errorsx.New("auth_invalid", "credential rejected")

// ErrInvalidUsers is wrapped in ParseUsers errors for unparseable
// "user:pass,user:pass" credential strings.
var ErrInvalidUsers = errors.New("guard: invalid users string")
