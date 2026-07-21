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

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating oauth_clients, rooted so
// its .sql files sit at fsys root (data/migration.New globs the root, not
// subdirectories). Apply via data/migration under its own version table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres oauthserver.Store. The pool's lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Postgres client registry. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const columns = `id, name, secret_hash, scopes, grants, redirect_uris, tenant_id, token_ttl_ms, revoked_at, created_at`

const createSQL = `
INSERT INTO oauth_clients (` + columns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

// Create inserts the client; an existing id yields ErrDuplicateClient.
func (s *Store) Create(ctx context.Context, c oauthserver.Client) error {
	_, err := s.pool.Exec(ctx, createSQL,
		c.ID, c.Name, c.SecretHash,
		emptyIfNil(c.Scopes), emptyIfNil(c.Grants), emptyIfNil(c.RedirectURIs),
		c.TenantID, c.TokenTTL.Milliseconds(), nullTime(c.RevokedAt), c.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return oauthserver.ErrDuplicateClient
	}
	return err
}

const getSQL = `SELECT ` + columns + ` FROM oauth_clients WHERE id = $1`

// Get fetches one client by id.
func (s *Store) Get(ctx context.Context, id string) (oauthserver.Client, error) {
	return scanClient(s.pool.QueryRow(ctx, getSQL, id))
}

const updateSQL = `
UPDATE oauth_clients
SET name = $2, secret_hash = $3, scopes = $4, grants = $5, redirect_uris = $6,
    tenant_id = $7, token_ttl_ms = $8, revoked_at = $9
WHERE id = $1`

// Update rewrites the client row; a missing id yields ErrClientNotFound.
func (s *Store) Update(ctx context.Context, c oauthserver.Client) error {
	tag, err := s.pool.Exec(ctx, updateSQL,
		c.ID, c.Name, c.SecretHash,
		emptyIfNil(c.Scopes), emptyIfNil(c.Grants), emptyIfNil(c.RedirectURIs),
		c.TenantID, c.TokenTTL.Milliseconds(), nullTime(c.RevokedAt))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oauthserver.ErrClientNotFound
	}
	return nil
}

// List statement shapes — see List for why there are two.
const (
	listSQL = `
SELECT ` + columns + ` FROM oauth_clients
ORDER BY created_at, id`

	listTenantSQL = `
SELECT ` + columns + ` FROM oauth_clients
WHERE tenant_id = $1
ORDER BY created_at, id`
)

// List returns the tenant's clients; tenantID "" returns all.
func (s *Store) List(ctx context.Context, tenantID string) ([]oauthserver.Client, error) {
	// Two static shapes instead of `($1 = '' OR tenant_id = $1)`: under a
	// generic plan (which pgx-prepared statements reach after five
	// executions) Postgres cannot prune that OR and stops using
	// oauth_clients_tenant_idx.
	sql, args := listSQL, []any(nil)
	if tenantID != "" {
		sql, args = listTenantSQL, []any{tenantID}
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []oauthserver.Client{}
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Delete removes the client row; deleting a missing id is a no-op.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM oauth_clients WHERE id = $1`, id)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanClient(row rowScanner) (oauthserver.Client, error) {
	var c oauthserver.Client
	var ttlMS int64
	var revoked *time.Time
	err := row.Scan(&c.ID, &c.Name, &c.SecretHash, &c.Scopes, &c.Grants,
		&c.RedirectURIs, &c.TenantID, &ttlMS, &revoked, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return oauthserver.Client{}, oauthserver.ErrClientNotFound
	}
	if err != nil {
		return oauthserver.Client{}, err
	}
	c.TokenTTL = time.Duration(ttlMS) * time.Millisecond
	if revoked != nil {
		c.RevokedAt = *revoked
	}
	return c, nil
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// emptyIfNil keeps nil slices out of pgx array encoding.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
