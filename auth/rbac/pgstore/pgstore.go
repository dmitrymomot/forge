package pgstore

import (
	"context"
	"embed"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/auth/rbac"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_rbac_assignments, rooted
// so its .sql files sit at fsys root (data/migration.New globs the root). Apply
// via data/migration under its own version table ("forge_rbac_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of rbac.Store. The pool's lifecycle is
// the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ rbac.Store = (*Store)(nil)

// New builds a Postgres rbac assignment Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// RolesFor returns the roles assigned to subject within tenant.
func (s *Store) RolesFor(ctx context.Context, tenant, subject string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT role FROM forge_rbac_assignments WHERE tenant = $1 AND subject = $2`, tenant, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

// Assign grants roles to subject within tenant. Idempotent (ON CONFLICT DO
// NOTHING) and batched in one round-trip.
func (s *Store) Assign(ctx context.Context, tenant, subject string, roles []string) error {
	if len(roles) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, role := range roles {
		batch.Queue(
			`INSERT INTO forge_rbac_assignments (tenant, subject, role)
			 VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, tenant, subject, role)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range roles {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// Unassign revokes roles from subject within tenant.
func (s *Store) Unassign(ctx context.Context, tenant, subject string, roles []string) error {
	if len(roles) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM forge_rbac_assignments WHERE tenant = $1 AND subject = $2 AND role = ANY($3)`,
		tenant, subject, roles)
	return err
}
