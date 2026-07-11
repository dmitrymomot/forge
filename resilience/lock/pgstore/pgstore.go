package pgstore

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_locks + its fence
// sequence, rooted so its .sql files sit at fsys root (data/migration.New
// globs fsys's root, not subdirectories). Apply via data/migration under its
// own version table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

type config struct{}

// Option configures the Store (reserved for future use).
type Option func(*config)

// Store is a Postgres table-lease implementation of lock.Store. Expiry is
// compared against the database's own now(), so it is immune to cross-node
// clock skew. The pool's lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Postgres lock Store. Apply Migrations first.
func New(pool *pgxpool.Pool, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{pool: pool}
}

const acquireSQL = `
INSERT INTO forge_locks (key, owner, expires_at, fence)
VALUES ($1, $2, now() + ($3::bigint * interval '1 millisecond'), nextval('forge_locks_fence_seq'))
ON CONFLICT (key) DO UPDATE
    SET owner = EXCLUDED.owner, expires_at = EXCLUDED.expires_at, fence = EXCLUDED.fence
    WHERE forge_locks.expires_at <= now() OR forge_locks.owner = EXCLUDED.owner
RETURNING fence`

// Acquire claims key for owner until now+ttl, returning a monotonic fencing
// token on success. ok is false if another live owner holds key.
func (s *Store) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (uint64, bool, error) {
	var fence uint64
	err := s.pool.QueryRow(ctx, acquireSQL, key, owner, ttl.Milliseconds()).Scan(&fence)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil // held by another live owner
	}
	if err != nil {
		return 0, false, err
	}
	return fence, true, nil
}

// Refresh extends the lease iff owner still holds key; ok is false if lost.
func (s *Store) Refresh(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forge_locks SET expires_at = now() + ($3::bigint * interval '1 millisecond')
		 WHERE key = $1 AND owner = $2 AND expires_at > now()`,
		key, owner, ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Release frees key iff held by owner (no-op otherwise).
func (s *Store) Release(ctx context.Context, key, owner string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM forge_locks WHERE key = $1 AND owner = $2`, key, owner)
	return err
}
