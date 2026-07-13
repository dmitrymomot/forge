package pgstore

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/auth/totp"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_totp, rooted so its
// .sql files sit at fsys root (data/migration.New globs the fsys root, not
// subdirectories). Apply via data/migration under its own version table
// ("forge_totp_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of totp.Store. The pool's lifecycle
// is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ totp.Store = (*Store)(nil)

// New builds a Postgres totp Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Get returns the record, or totp.ErrNotFound.
func (s *Store) Get(ctx context.Context, tenant, subject string) (*totp.Record, error) {
	var r totp.Record
	var lastUsed *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT secret, confirmed, last_used_at, backup_hashes
		 FROM forge_totp WHERE tenant = $1 AND subject = $2`,
		tenant, subject).Scan(&r.Secret, &r.Confirmed, &lastUsed, &r.BackupHashes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, totp.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if lastUsed != nil {
		r.LastUsedAt = *lastUsed
	}
	return &r, nil
}

// Save upserts the record (full replace of mutable columns).
func (s *Store) Save(ctx context.Context, tenant, subject string, r *totp.Record) error {
	hashes := r.BackupHashes
	if hashes == nil {
		hashes = [][]byte{}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO forge_totp (tenant, subject, secret, confirmed, last_used_at, backup_hashes)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant, subject) DO UPDATE SET
		   secret = EXCLUDED.secret,
		   confirmed = EXCLUDED.confirmed,
		   last_used_at = EXCLUDED.last_used_at,
		   backup_hashes = EXCLUDED.backup_hashes,
		   updated_at = now()`,
		tenant, subject, r.Secret, r.Confirmed, nullTime(r.LastUsedAt), hashes)
	return err
}

// Delete removes the record; absent is a no-op.
func (s *Store) Delete(ctx context.Context, tenant, subject string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM forge_totp WHERE tenant = $1 AND subject = $2`, tenant, subject)
	return err
}

// MarkUsed atomically advances last_used_at iff the stored value is
// earlier (or NULL). The condition lives in the WHERE clause, so
// concurrent verifies of the same step resolve to exactly one winner.
func (s *Store) MarkUsed(ctx context.Context, tenant, subject string, usedAt time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forge_totp SET last_used_at = $3, updated_at = now()
		 WHERE tenant = $1 AND subject = $2
		   AND (last_used_at IS NULL OR last_used_at < $3)`,
		tenant, subject, usedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ConsumeBackup atomically removes hash if present; the ANY guard makes
// concurrent consumes of the same code resolve to exactly one winner.
func (s *Store) ConsumeBackup(ctx context.Context, tenant, subject string, hash []byte) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forge_totp SET backup_hashes = array_remove(backup_hashes, $3), updated_at = now()
		 WHERE tenant = $1 AND subject = $2 AND $3 = ANY(backup_hashes)`,
		tenant, subject, hash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// DeleteTenant removes every record in tenant — exact match on the leading
// PK column, no pattern matching.
func (s *Store) DeleteTenant(ctx context.Context, tenant string) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM forge_totp WHERE tenant = $1`, tenant)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
