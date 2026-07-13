package magiclink

import "errors"

// Sentinel errors returned by Peek and Redeem. All are errors.Is-matchable;
// runtime wrapping keeps the underlying cause inspectable for logs.
var (
	// ErrInvalid is returned when a link is malformed, its signature does not
	// verify, or it was issued for a different purpose.
	ErrInvalid = errors.New("magiclink: invalid link")
	// ErrExpired is returned when the link's TTL has passed.
	ErrExpired = errors.New("magiclink: link expired")
	// ErrUsed is returned when a single-use link has already been redeemed.
	ErrUsed = errors.New("magiclink: link already used")
	// ErrScopeMismatch is returned when a scoped link is verified outside the
	// tenant scope it was issued in.
	ErrScopeMismatch = errors.New("magiclink: scope mismatch")
	// ErrStore is returned when the single-use store fails; redemption fails
	// closed.
	ErrStore = errors.New("magiclink: store operation failed")
)
