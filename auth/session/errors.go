package session

import "errors"

var (
	// ErrNotFound means no record exists for the presented token.
	ErrNotFound = errors.New("session: not found")
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

	// ErrBadIdle means Idle or RememberIdle is not positive.
	ErrBadIdle = errors.New("session: idle ttl must be positive")
	// ErrBadMaxTTL means an absolute lifetime is negative or below its idle ttl.
	ErrBadMaxTTL = errors.New("session: max ttl must be zero or above the idle ttl")
	// ErrBadTouch means Touch is negative or exceeds the idle ttl.
	ErrBadTouch = errors.New("session: touch interval must be zero or within the idle ttl")
	// ErrTouchUnsupported means Touch is configured but the Store has no Toucher.
	ErrTouchUnsupported = errors.New("session: touch configured but store does not implement Toucher")
)
