package pgstore

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/web/smartlink"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_smart_links, rooted so
// its .sql files sit at fsys root (data/migration.New globs fsys's root, not
// subdirectories). Apply via data/migration under its own version table
// ("forge_smartlink_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of smartlink.Store. The pool's
// lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ smartlink.Store = (*Store)(nil)

// New builds a Postgres smartlink Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `code, target, ref, metadata, tenant, created_at, expires_at, deactivated_at`

const createSQL = `
INSERT INTO forge_smart_links (` + cols + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

// Create inserts l keyed by l.Code. A colliding code yields smartlink.ErrDuplicate.
// Metadata is stored as its JSON encoding — a nil map as jsonb 'null' — so a
// nil round-trips back as nil and an empty map as an empty map, matching the
// MemoryStore reference contract exactly.
func (s *Store) Create(ctx context.Context, l smartlink.Link) error {
	meta, err := json.Marshal(l.Metadata)
	if err != nil {
		return err // unreachable: map[string]string always marshals
	}
	_, err = s.pool.Exec(ctx, createSQL,
		l.Code, l.Target, l.Ref, meta, l.Tenant,
		l.CreatedAt, nullTime(l.ExpiresAt), nullTime(l.DeactivatedAt))
	if postgres.IsUniqueViolation(err) {
		return smartlink.ErrDuplicate
	}
	return err
}

// Get returns the Link stored under code, or smartlink.ErrNotFound.
func (s *Store) Get(ctx context.Context, code string) (smartlink.Link, error) {
	return scanLink(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_smart_links WHERE code = $1`, code))
}

// List returns Links matching f, newest first (created_at descending, code
// ascending on ties). An empty Filter.Tenant matches every tenant; a zero
// Filter.Limit applies no cap.
func (s *Store) List(ctx context.Context, f smartlink.Filter) ([]smartlink.Link, error) {
	// The tenant predicate is added only when set, rather than the static
	// `($1 = '' OR tenant = $1)` idiom: under a generic plan (which
	// pgx-prepared statements reach after five executions) Postgres cannot
	// prune that OR and stops using forge_smart_links_tenant_created_idx.
	sql := `SELECT ` + cols + ` FROM forge_smart_links`
	var args []any
	if f.Tenant != "" {
		args = append(args, f.Tenant)
		sql += ` WHERE tenant = $1`
	}
	sql += ` ORDER BY created_at DESC, code ASC`
	if f.Limit > 0 {
		args = append(args, f.Limit)
		sql += ` LIMIT $` + strconv.Itoa(len(args))
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Non-nil empty (not nil) on zero rows, matching the memory store so
	// callers see identical List results whichever Store backs the Manager.
	out := []smartlink.Link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// The point statements come in unscoped/tenant-scoped pairs instead of the
// optional-tenant OR idiom List used to share. Perf is not the driver here
// — code is the primary key, so these are index point lookups under any
// plan — but the pair keeps the package free of the OR idiom so it cannot
// be copied back into a query where it does defeat an index (see List).
const (
	deactivateSQL       = `UPDATE forge_smart_links SET deactivated_at = $2 WHERE code = $1`
	deactivateTenantSQL = deactivateSQL + ` AND tenant = $3`
)

// Deactivate sets DeactivatedAt to at on code, scoped to tenant. A zero at
// is rejected per the Store contract — it would write NULL and silently
// reactivate the link.
func (s *Store) Deactivate(ctx context.Context, code, tenant string, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("%w: Deactivate needs a non-zero time", smartlink.ErrInvalidLink)
	}
	if tenant == "" {
		return s.exec(ctx, deactivateSQL, code, at)
	}
	return s.exec(ctx, deactivateTenantSQL, code, at, tenant)
}

const (
	activateSQL       = `UPDATE forge_smart_links SET deactivated_at = NULL WHERE code = $1`
	activateTenantSQL = activateSQL + ` AND tenant = $2`
)

// Activate clears DeactivatedAt on code, scoped to tenant.
func (s *Store) Activate(ctx context.Context, code, tenant string) error {
	if tenant == "" {
		return s.exec(ctx, activateSQL, code)
	}
	return s.exec(ctx, activateTenantSQL, code, tenant)
}

const (
	deleteSQL       = `DELETE FROM forge_smart_links WHERE code = $1`
	deleteTenantSQL = deleteSQL + ` AND tenant = $2`
)

// Delete removes code, scoped to tenant.
func (s *Store) Delete(ctx context.Context, code, tenant string) error {
	if tenant == "" {
		return s.exec(ctx, deleteSQL, code)
	}
	return s.exec(ctx, deleteTenantSQL, code, tenant)
}

// exec runs sql and maps a zero-row result to smartlink.ErrNotFound: when a
// tenant is set its predicate is inline in sql, so a mismatch and a missing
// code are indistinguishable and both surface the same error.
func (s *Store) exec(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return smartlink.ErrNotFound
	}
	return nil
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanLink(r row) (smartlink.Link, error) {
	var l smartlink.Link
	var exp, deact *time.Time
	err := r.Scan(&l.Code, &l.Target, &l.Ref, &l.Metadata, &l.Tenant,
		&l.CreatedAt, &exp, &deact)
	if errors.Is(err, pgx.ErrNoRows) {
		return smartlink.Link{}, smartlink.ErrNotFound
	}
	if err != nil {
		return smartlink.Link{}, err
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
