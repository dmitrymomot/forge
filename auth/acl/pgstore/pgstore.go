package pgstore

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/acl"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_acl_entries, rooted so
// its .sql files sit at fsys root (data/migration.New globs the root). Apply
// via data/migration under its own version table ("forge_acl_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of acl.Store. The pool's lifecycle is
// the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ acl.Store = (*Store)(nil)

// New builds a Postgres ACL entry Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func effectText(e access.Effect) (string, error) {
	switch e {
	case access.Allow:
		return "allow", nil
	case access.Deny:
		return "deny", nil
	default:
		return "", fmt.Errorf("%w: effect %v", acl.ErrInvalidEntry, e)
	}
}

func textEffect(s string) (access.Effect, error) {
	switch s {
	case "allow":
		return access.Allow, nil
	case "deny":
		return access.Deny, nil
	default: // unreachable behind the CHECK constraint
		return access.Abstain, fmt.Errorf("%w: effect %q", acl.ErrInvalidEntry, s)
	}
}

func scanEntries(rows pgx.Rows, subject string) ([]acl.Entry, error) {
	defer rows.Close()
	var entries []acl.Entry
	for rows.Next() {
		var e acl.Entry
		var effect string
		if err := rows.Scan(&e.ResourceType, &e.ResourceID, &e.Action, &effect); err != nil {
			return nil, err
		}
		eff, err := textEffect(effect)
		if err != nil {
			return nil, err
		}
		e.Subject = subject
		e.Effect = eff
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// EntriesFor returns subject's entries on (resourceType, resourceID) plus the
// type-wide entries (empty resource_id), in one primary-key-indexed query.
func (s *Store) EntriesFor(ctx context.Context, tenant, subject, resourceType, resourceID string) ([]acl.Entry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT resource_type, resource_id, action, effect FROM forge_acl_entries
		 WHERE tenant = $1 AND subject = $2 AND resource_type = $3 AND resource_id IN ($4, '')`,
		tenant, subject, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows, subject)
}

// ListFor returns all of subject's entries within tenant.
func (s *Store) ListFor(ctx context.Context, tenant, subject string) ([]acl.Entry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT resource_type, resource_id, action, effect FROM forge_acl_entries
		 WHERE tenant = $1 AND subject = $2`,
		tenant, subject)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows, subject)
}

// Put upserts entries by key, batched in one round-trip; a conflicting key
// has its effect overwritten (grant↔deny flips in place).
func (s *Store) Put(ctx context.Context, tenant string, entries []acl.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, e := range entries {
		effect, err := effectText(e.Effect)
		if err != nil {
			return err
		}
		batch.Queue(
			`INSERT INTO forge_acl_entries (tenant, subject, resource_type, resource_id, action, effect)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (tenant, subject, resource_type, resource_id, action)
			 DO UPDATE SET effect = EXCLUDED.effect`,
			tenant, e.Subject, e.ResourceType, e.ResourceID, e.Action, effect)
	}
	br := s.pool.SendBatch(ctx, batch)
	for range entries {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return err
		}
	}
	return br.Close()
}

// Delete removes subject's entries on the resource for the given actions.
func (s *Store) Delete(ctx context.Context, tenant, subject, resourceType, resourceID string, actions []string) error {
	if len(actions) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM forge_acl_entries
		 WHERE tenant = $1 AND subject = $2 AND resource_type = $3 AND resource_id = $4 AND action = ANY($5)`,
		tenant, subject, resourceType, resourceID, actions)
	return err
}
