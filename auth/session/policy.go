package session

import (
	"context"
	"errors"
	"net/http"
)

// Policy inspects a loaded session against the current request. Policies run in
// the order they are registered and short-circuit on the first non-nil return.
//
// Returning nil continues; Deny answers 401 and leaves the record intact;
// Revoke deletes the record and answers 401; any other error fails closed with
// a 500. All policies are request-time, which is why the Manager carries no
// hook machinery at all.
type Policy func(ctx context.Context, r *http.Request, s *Session) error

// DenyError rejects the request without destroying the session.
type DenyError struct{ reason string }

// Error implements error.
func (e *DenyError) Error() string { return "session: denied: " + e.reason }

// Reason returns the human-readable cause, surfaced to logs and the responder.
func (e *DenyError) Reason() string { return e.reason }

// RevokeError rejects the request and destroys the session.
type RevokeError struct{ reason string }

// Error implements error.
func (e *RevokeError) Error() string { return "session: revoked: " + e.reason }

// Reason returns the human-readable cause, surfaced to logs and the responder.
func (e *RevokeError) Reason() string { return e.reason }

// Deny builds the "reject, keep the record" outcome.
func Deny(reason string) error { return &DenyError{reason: reason} }

// Revoke builds the "reject and destroy the record" outcome.
func Revoke(reason string) error { return &RevokeError{reason: reason} }

// IsDeny reports whether err is a Deny and returns its reason.
func IsDeny(err error) (string, bool) {
	d, ok := errors.AsType[*DenyError](err)
	if !ok {
		return "", false
	}
	return d.reason, true
}

// IsRevoke reports whether err is a Revoke and returns its reason.
func IsRevoke(err error) (string, bool) {
	r, ok := errors.AsType[*RevokeError](err)
	if !ok {
		return "", false
	}
	return r.reason, true
}
