package dnsverify

import "errors"

var (
	// ErrLookup wraps a genuine resolver failure (timeout, SERVFAIL, other
	// temporary DNS error). It is distinct from "the record is not published
	// yet", which Verify reports as an unverified Result with a nil error.
	ErrLookup = errors.New("dnsverify: lookup failed")

	// ErrInvalidChallenge marks a malformed Challenge — unknown Record, empty
	// Host, or empty Expect — rejected before any lookup.
	ErrInvalidChallenge = errors.New("dnsverify: invalid challenge")

	// ErrInvalidConfig marks a Config that fails Validate at construction.
	ErrInvalidConfig = errors.New("dnsverify: invalid config")
)
