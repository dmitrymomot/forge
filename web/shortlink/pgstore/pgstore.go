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

	"github.com/dmitrymomot/forge/web/shortlink"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_short_links, rooted
// so its .sql files sit at fsys root (data/migration.New globs fsys's root,
// not subdirectories). Apply via data/migration under its own version
// table ("forge_shortlink_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of shortlink.Store. The pool's
// lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ shortlink.Store = (*Store)(nil)

// New builds a Postgres shortlink Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `code, url, tenant, created_at, expires_at, deactivated_at`

// Create inserts l. A colliding code yields shortlink.ErrDuplicate.
func (s *Store) Create(ctx context.Context, l shortlink.Link) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO forge_short_links (`+cols+`) VALUES ($1, $2, $3, $4, $5, $6)`,
		l.Code, l.URL, l.Tenant, l.CreatedAt, nullTime(l.ExpiresAt), nullTime(l.DeactivatedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return shortlink.ErrDuplicate
	}
	return err
}

// Get returns the record for code, or shortlink.ErrNotFound. This is the
// resolve hot path: one point lookup on the primary key.
func (s *Store) Get(ctx context.Context, code string) (shortlink.Link, error) {
	return scanLink(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_short_links WHERE code = $1`, code))
}

// List returns records matching f, newest first (created_at DESC, code ASC
// on ties). The tenant-filtered and unfiltered shapes are separate static
// statements so the tenant-leading index serves the filtered one and the
// planner never has to fold a `$1 = ”` catch-all out of a generic plan.
func (s *Store) List(ctx context.Context, f shortlink.Filter) ([]shortlink.Link, error) {
	var rows pgx.Rows
	var err error
	if f.Tenant == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT `+cols+` FROM forge_short_links
			 ORDER BY created_at DESC, code ASC
			 LIMIT NULLIF($1, 0)`, f.Limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT `+cols+` FROM forge_short_links
			 WHERE tenant = $1
			 ORDER BY created_at DESC, code ASC
			 LIMIT NULLIF($2, 0)`, f.Tenant, f.Limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Non-nil empty (not nil) on zero rows, matching the memory store so
	// callers see identical List results whichever Store backs the Manager.
	out := []shortlink.Link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Deactivate sets deactivated_at, or returns shortlink.ErrNotFound. The
// tenant predicate is part of the statement so scope confinement is atomic
// with the mutation; a zero at maps to NULL, matching the memory store's
// zero-means-active semantics.
func (s *Store) Deactivate(ctx context.Context, code, tenant string, at time.Time) error {
	return s.exec(ctx,
		`UPDATE forge_short_links SET deactivated_at = $2 WHERE code = $1 AND ($3 = '' OR tenant = $3)`,
		code, nullTime(at), tenant)
}

// Activate clears deactivated_at, or returns shortlink.ErrNotFound.
func (s *Store) Activate(ctx context.Context, code, tenant string) error {
	return s.exec(ctx,
		`UPDATE forge_short_links SET deactivated_at = NULL WHERE code = $1 AND ($2 = '' OR tenant = $2)`,
		code, tenant)
}

// Delete removes the record, or returns shortlink.ErrNotFound.
func (s *Store) Delete(ctx context.Context, code, tenant string) error {
	return s.exec(ctx,
		`DELETE FROM forge_short_links WHERE code = $1 AND ($2 = '' OR tenant = $2)`,
		code, tenant)
}

func (s *Store) exec(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shortlink.ErrNotFound
	}
	return nil
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanLink(r row) (shortlink.Link, error) {
	var l shortlink.Link
	var exp, deact *time.Time
	err := r.Scan(&l.Code, &l.URL, &l.Tenant, &l.CreatedAt, &exp, &deact)
	if errors.Is(err, pgx.ErrNoRows) {
		return shortlink.Link{}, shortlink.ErrNotFound
	}
	if err != nil {
		return shortlink.Link{}, err
	}
	l.ExpiresAt = deref(exp)
	l.DeactivatedAt = deref(deact)
	return l, nil
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
