package session

import "errors"

var (
	// ErrNotFound means no record exists for the presented token.
	ErrNotFound = errors.New("session: not found")
	// ErrExists means Create found a record already stored under the token.
	ErrExists = errors.New("session: record already exists")
	// ErrExpired means the record exists but its deadline has passed.
	ErrExpired = errors.New("session: expired")
	// ErrUnsupported means the configured Store lacks the capability this call needs.
	ErrUnsupported = errors.New("session: store capability unsupported")
	// ErrNoEmbed means a Transport can read a token but cannot write one back.
	ErrNoEmbed = errors.New("session: transport cannot embed")
	// ErrNoSession means no session is present in the context.
	ErrNoSession = errors.New("session: no session in context")
	// ErrNoStore means New was called without WithStore.
	ErrNoStore = errors.New("session: no store configured")
	// ErrAnonymous means the call requires an authenticated session.
	ErrAnonymous = errors.New("session: session is anonymous")
	// ErrUserMismatch means Authenticate was asked to switch an authenticated
	// session to a different user. Log the first user out instead: carrying one
	// user's payload into another's session leaks whatever the first cached.
	ErrUserMismatch = errors.New("session: session is bound to another user")
	// ErrDenied is what the middleware hands the responder when a policy denies:
	// the policy's reason goes to logs, never into the response body.
	ErrDenied = errors.New("session: denied by policy")
	// ErrRevoked is the responder-facing counterpart of ErrDenied for revocations.
	ErrRevoked = errors.New("session: revoked by policy")
	// ErrScope means a configured scope hook errored or yielded an empty scope. A
	// scoped operation fails closed rather than touching an unscoped bucket.
	ErrScope = errors.New("session: scope resolution failed")

	// ErrBadIdle means Idle or RememberIdle is not positive.
	ErrBadIdle = errors.New("session: idle ttl must be positive")
	// ErrBadMaxTTL means an absolute lifetime is negative or below its idle ttl.
	ErrBadMaxTTL = errors.New("session: max ttl must be zero or above the idle ttl")
	// ErrBadTouch means Touch is negative or not below both idle ttls.
	ErrBadTouch = errors.New("session: touch interval must be zero or below both idle ttls")
)
