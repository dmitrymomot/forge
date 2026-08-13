package apikey

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// SaveFunc persists a newly minted key. It must report ErrDuplicate for an
// existing ID or Hash instead of overwriting.
type SaveFunc func(ctx context.Context, k Key) error

// LoadFunc reads one key by record id. It reports ErrNotFound when absent.
type LoadFunc func(ctx context.Context, keyID id.UUID) (Key, error)

// LoadByHashFunc reads one key by the hex SHA-256 of the presented
// plaintext — the single point lookup on the verify path. It reports
// ErrNotFound when absent.
type LoadByHashFunc func(ctx context.Context, hash string) (Key, error)

// ListFunc reads the keys matching f, newest first (UUIDv7 id order; ties
// within one millisecond are unordered).
type ListFunc func(ctx context.Context, f Filter) ([]Key, error)

// RevokeFunc stamps a key revoked at `at`. Revocation is terminal.
type RevokeFunc func(ctx context.Context, keyID id.UUID, at time.Time) error

// TouchFunc stamps a key's last-used-at. Verify calls it best-effort: its
// error never fails authentication. A nil TouchFunc disables tracking.
type TouchFunc func(ctx context.Context, keyID id.UUID, at time.Time) error

// SwapFunc performs the whole rotation write as one atomic unit: persist
// replacement and set the old key's ExpiresAt to oldExpiresAt. Doing both
// in one transaction is why Rotate needs no compensation — a failure
// leaves the old key untouched and no orphan replacement behind.
type SwapFunc func(ctx context.Context, oldID id.UUID, oldExpiresAt time.Time, replacement Key) error
