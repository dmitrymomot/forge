package rng

import (
	"context"
	"time"
)

// Seed pair statuses.
const (
	StatusActive   = "active"
	StatusRevealed = "revealed"
)

// Record is the storage shape of one seed pair. ServerSeed is always
// present — derivation needs it until reveal — so the backing table must
// be treated as secret material.
type Record struct {
	CreatedAt  time.Time
	RevealedAt time.Time // zero until revealed
	ID         string
	Scope      string
	PlayerID   string
	ClientSeed string
	Status     string // StatusActive or StatusRevealed
	Algorithm  string // Algorithm the pair derives with ("rng/v1")
	ServerSeed []byte
	Nonce      uint64 // next unused; ConsumeNonce returns the consumed value
}

// Store persists seed pairs. Implementations must be safe for concurrent
// use and must enforce at most one active record per (scope, playerID).
type Store interface {
	// Active returns the active record for (scope, playerID), or ErrNotFound.
	Active(ctx context.Context, scope, playerID string) (Record, error)
	// Create inserts r. ErrExists when an active record already exists
	// for (r.Scope, r.PlayerID) or the id collides.
	Create(ctx context.Context, r Record) error
	// ConsumeNonce atomically increments the active record's nonce and
	// returns the record with Nonce set to the consumed (pre-increment)
	// value; ErrNotFound when the player has no active record.
	ConsumeNonce(ctx context.Context, scope, playerID string) (Record, error)
	// Reveal marks the record revealed at the given time and returns it.
	// Idempotent: revealing a revealed record returns it unchanged.
	Reveal(ctx context.Context, scope, id string, at time.Time) (Record, error)
	// Get returns the record by id within scope, or ErrNotFound.
	Get(ctx context.Context, scope, id string) (Record, error)
}
