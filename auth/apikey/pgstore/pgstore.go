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

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_api_keys, rooted so
// its .sql files sit at fsys root (data/migration.New globs fsys's root,
// not subdirectories). Apply via data/migration under its own version
// table ("forge_apikey_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of apikey.Store. The pool's
// lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ apikey.Store = (*Store)(nil)

// New builds a Postgres apikey Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, hash, preview, name, subject, tenant, scopes, meta, created_at, expires_at, last_used_at, revoked_at`

const createSQL = `
INSERT INTO forge_api_keys (` + cols + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

// Create inserts k. A colliding id or hash yields apikey.ErrDuplicate.
func (s *Store) Create(ctx context.Context, k apikey.Key) error {
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	meta := k.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	_, err := s.pool.Exec(ctx, createSQL,
		k.ID, k.Hash, k.Preview, k.Name, k.Subject, k.Tenant, scopes, meta,
		k.CreatedAt, nullTime(k.ExpiresAt), nullTime(k.LastUsedAt), nullTime(k.RevokedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return apikey.ErrDuplicate
	}
	return err
}

// Get returns the record for keyID, or apikey.ErrNotFound.
func (s *Store) Get(ctx context.Context, keyID id.UUID) (apikey.Key, error) {
	return scanKey(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_api_keys WHERE id = $1`, keyID))
}

// GetByHash returns the record whose hash matches, or apikey.ErrNotFound.
// This is the verification hot path: one point lookup on the unique index.
func (s *Store) GetByHash(ctx context.Context, hash string) (apikey.Key, error) {
	return scanKey(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_api_keys WHERE hash = $1`, hash))
}

// List returns records matching f, newest first (UUIDv7 ids are
// time-ordered, so id DESC is creation order).
func (s *Store) List(ctx context.Context, f apikey.Filter) ([]apikey.Key, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+cols+` FROM forge_api_keys
		 WHERE ($1 = '' OR tenant = $1) AND ($2 = '' OR subject = $2)
		 ORDER BY id DESC`, f.Tenant, f.Subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Non-nil empty (not nil) on zero rows, matching the memory store so
	// callers see identical List results whichever Store backs the Manager.
	out := []apikey.Key{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Revoke sets revoked_at, or returns apikey.ErrNotFound.
func (s *Store) Revoke(ctx context.Context, keyID id.UUID, at time.Time) error {
	return s.setTime(ctx, `UPDATE forge_api_keys SET revoked_at = $2 WHERE id = $1`, keyID, at)
}

// Expire sets expires_at (rotation grace), or returns apikey.ErrNotFound.
func (s *Store) Expire(ctx context.Context, keyID id.UUID, at time.Time) error {
	return s.setTime(ctx, `UPDATE forge_api_keys SET expires_at = $2 WHERE id = $1`, keyID, at)
}

// Touch sets last_used_at, or returns apikey.ErrNotFound.
func (s *Store) Touch(ctx context.Context, keyID id.UUID, at time.Time) error {
	return s.setTime(ctx, `UPDATE forge_api_keys SET last_used_at = $2 WHERE id = $1`, keyID, at)
}

func (s *Store) setTime(ctx context.Context, sql string, keyID id.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, sql, keyID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apikey.ErrNotFound
	}
	return nil
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanKey(r row) (apikey.Key, error) {
	var k apikey.Key
	var exp, lu, rv *time.Time
	err := r.Scan(&k.ID, &k.Hash, &k.Preview, &k.Name, &k.Subject, &k.Tenant,
		&k.Scopes, &k.Meta, &k.CreatedAt, &exp, &lu, &rv)
	if errors.Is(err, pgx.ErrNoRows) {
		return apikey.Key{}, apikey.ErrNotFound
	}
	if err != nil {
		return apikey.Key{}, err
	}
	k.ExpiresAt = deref(exp)
	k.LastUsedAt = deref(lu)
	k.RevokedAt = deref(rv)
	return k, nil
}

// deref maps SQL NULL back to the zero time.
func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
