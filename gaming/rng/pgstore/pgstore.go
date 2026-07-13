package pgstore

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/gaming/rng"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_rng_seeds, rooted
// so its .sql files sit at fsys root (data/migration.New globs fsys's
// root, not subdirectories). Apply via data/migration under its own
// version table ("forge_rng_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of rng.Store. The pool's lifecycle
// is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ rng.Store = (*Store)(nil)

// New builds a Postgres rng Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, scope, player_id, server_seed, client_seed, nonce, status, algorithm, created_at, revealed_at`

// Active returns the active record for (scope, playerID).
func (s *Store) Active(ctx context.Context, scope, playerID string) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM forge_rng_seeds WHERE scope = $1 AND player_id = $2 AND status = 'active'`,
		scope, playerID))
}

// Create inserts r. The partial unique index on active (scope, player_id)
// and the primary key both surface as rng.ErrExists.
func (s *Store) Create(ctx context.Context, r rng.Record) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO forge_rng_seeds (`+cols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.Scope, r.PlayerID, r.ServerSeed, r.ClientSeed, int64(r.Nonce), r.Status, r.Algorithm,
		r.CreatedAt, nullTime(r.RevealedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return rng.ErrExists
	}
	return err
}

// ConsumeNonce is the hot path: one conditional UPDATE ... RETURNING on
// the active row. RETURNING sees the post-update row, so the consumed
// (pre-increment) value is nonce - 1.
func (s *Store) ConsumeNonce(ctx context.Context, scope, playerID string) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`UPDATE forge_rng_seeds SET nonce = nonce + 1
		 WHERE scope = $1 AND player_id = $2 AND status = 'active'
		 RETURNING id, scope, player_id, server_seed, client_seed, nonce - 1, status, algorithm, created_at, revealed_at`,
		scope, playerID))
}

// Reveal marks the record revealed; COALESCE keeps the first reveal time,
// making repeat reveals idempotent.
func (s *Store) Reveal(ctx context.Context, scope, id string, at time.Time) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`UPDATE forge_rng_seeds SET status = 'revealed', revealed_at = COALESCE(revealed_at, $3)
		 WHERE scope = $1 AND id = $2
		 RETURNING `+cols, scope, id, at))
}

// Get returns the record by id within scope.
func (s *Store) Get(ctx context.Context, scope, id string) (rng.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM forge_rng_seeds WHERE scope = $1 AND id = $2`, scope, id))
}

func scanRecord(row pgx.Row) (rng.Record, error) {
	var rec rng.Record
	var nonce int64
	var revealed *time.Time
	err := row.Scan(&rec.ID, &rec.Scope, &rec.PlayerID, &rec.ServerSeed, &rec.ClientSeed,
		&nonce, &rec.Status, &rec.Algorithm, &rec.CreatedAt, &revealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return rng.Record{}, rng.ErrNotFound
	}
	if err != nil {
		return rng.Record{}, err
	}
	rec.Nonce = uint64(nonce)
	if revealed != nil {
		rec.RevealedAt = *revealed
	}
	return rec, nil
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
