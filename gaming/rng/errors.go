package rng

import "errors"

var (
	// ErrInvalidSeed reports a server seed that is not exactly 32 bytes.
	ErrInvalidSeed = errors.New("rng: server seed must be exactly 32 bytes")

	// ErrInvalidClientSeed reports a client seed outside 1-64 chars of [A-Za-z0-9_-].
	ErrInvalidClientSeed = errors.New("rng: client seed must be 1-64 chars of [A-Za-z0-9_-]")

	// ErrInvalidTable reports invalid drop-table construction: no entries,
	// empty or duplicate keys, zero weights, weight-sum overflow, or bad
	// pity configuration.
	ErrInvalidTable = errors.New("rng: invalid table")

	// ErrNotFound reports an unknown seed id or a missing active pair.
	ErrNotFound = errors.New("rng: seed not found")

	// ErrExists reports a conflicting record: an active pair already
	// exists for the (scope, player), or the record id collides. Store
	// implementations return it from Create; the Manager consumes it
	// internally when racing get-or-create.
	ErrExists = errors.New("rng: active seed already exists")

	// ErrNoScope reports fail-closed tenancy: the configured scope hook
	// errored or returned an empty scope.
	ErrNoScope = errors.New("rng: scope unavailable")

	// ErrStore wraps store/driver failures surfaced by the Manager.
	ErrStore = errors.New("rng: store failure")
)
