package rng

import "errors"

var (
	// ErrInvalidSeed reports a server seed that is not exactly 32 bytes.
	ErrInvalidSeed = errors.New("rng: server seed must be exactly 32 bytes")

	// ErrInvalidClientSeed reports a client seed outside 1-64 chars of [A-Za-z0-9_-].
	ErrInvalidClientSeed = errors.New("rng: client seed must be 1-64 chars of [A-Za-z0-9_-]")
)
