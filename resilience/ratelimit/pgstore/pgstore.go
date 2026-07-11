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

// Migrations holds the goose migration that creates forge_ratelimit_counters,
// rooted so its .sql files sit at fsys root (data/migration.New globs fsys's
// root, not subdirectories). Apply it via data/migration under its own
// version table.
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

// Store is a durable Postgres counter Store implementing ratelimit.Store. It is
// the recommended backend for quota gauges. The pool's lifecycle is the
// caller's; Close is a no-op.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Postgres-backed counter Store. Apply Migrations first.
func New(pool *pgxpool.Pool, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{pool: pool}
}

const incrSQL = `
INSERT INTO forge_ratelimit_counters (key, val, expires_at)
VALUES ($1, $2, CASE WHEN $3::bigint > 0 THEN now() + ($3::text || ' milliseconds')::interval ELSE NULL END)
ON CONFLICT (key) DO UPDATE SET
    val = CASE
        WHEN forge_ratelimit_counters.expires_at IS NOT NULL AND forge_ratelimit_counters.expires_at <= now()
        THEN EXCLUDED.val
        ELSE forge_ratelimit_counters.val + EXCLUDED.val END,
    expires_at = CASE
        WHEN forge_ratelimit_counters.expires_at IS NOT NULL AND forge_ratelimit_counters.expires_at <= now()
        THEN EXCLUDED.expires_at
        ELSE forge_ratelimit_counters.expires_at END
RETURNING val`

// Incr adds delta, arming the TTL only on create or after expiry; a ttl <= 0
// stores a NULL (never-expiring) expiry.
func (s *Store) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	var v int64
	err := s.pool.QueryRow(ctx, incrSQL, key, delta, ttl.Milliseconds()).Scan(&v)
	return v, err
}

// Get returns the live counter, or 0 when absent or expired.
func (s *Store) Get(ctx context.Context, key string) (int64, error) {
	var v int64
	err := s.pool.QueryRow(ctx,
		`SELECT val FROM forge_ratelimit_counters WHERE key = $1 AND (expires_at IS NULL OR expires_at > now())`,
		key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// Reset deletes the counter.
func (s *Store) Reset(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM forge_ratelimit_counters WHERE key = $1`, key)
	return err
}

// Close is a no-op; the pool is owned by the caller.
func (s *Store) Close() error { return nil }
