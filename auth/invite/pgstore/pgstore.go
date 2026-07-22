package pgstore

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/auth/invite"
	"github.com/dmitrymomot/forge/core/id"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_invites, rooted so
// its .sql files sit at fsys root (data/migration.New globs fsys's root,
// not subdirectories). Apply via data/migration under its own version
// table ("forge_invite_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of invite.Store. The pool's
// lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ invite.Store = (*Store)(nil)

// New builds a Postgres invite Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, hash, email, tenant, role, created_at, expires_at, accepted_at, revoked_at`

const createSQL = `
INSERT INTO forge_invites (` + cols + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

// Create inserts inv. A colliding id or hash yields invite.ErrDuplicate.
func (s *Store) Create(ctx context.Context, inv invite.Invite) error {
	_, err := s.pool.Exec(ctx, createSQL,
		inv.ID, inv.Hash, inv.Email, inv.Tenant, inv.Role,
		inv.CreatedAt, inv.ExpiresAt, nullTime(inv.AcceptedAt), nullTime(inv.RevokedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return invite.ErrDuplicate
	}
	return err
}

// Get returns the record for inviteID, or invite.ErrNotFound.
func (s *Store) Get(ctx context.Context, inviteID id.UUID) (invite.Invite, error) {
	return scanInvite(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_invites WHERE id = $1`, inviteID))
}

// GetByHash returns the record whose hash matches, or invite.ErrNotFound.
// This is the accept path: one point lookup on the unique index.
func (s *Store) GetByHash(ctx context.Context, hash string) (invite.Invite, error) {
	return scanInvite(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM forge_invites WHERE hash = $1`, hash))
}

// List returns records matching f, newest first (UUIDv7 ids are
// time-ordered, so id DESC is creation order).
func (s *Store) List(ctx context.Context, f invite.Filter) ([]invite.Invite, error) {
	// The WHERE clause carries only the filters actually set, rather than
	// the static `($n = '' OR col = $n)` idiom: once a prepared statement
	// switches to a generic plan (pgx prepares every query, and Postgres
	// goes generic after five executions) those ORs cannot be pruned and
	// the planner stops using forge_invites_list_idx. The filter
	// combinations bound the statement cache at 8 shapes.
	var (
		conds []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.Tenant != "" {
		conds = append(conds, "tenant = "+arg(f.Tenant))
	}
	if f.Email != "" {
		conds = append(conds, "email = "+arg(f.Email))
	}
	if f.Pending {
		conds = append(conds, "accepted_at IS NULL AND revoked_at IS NULL AND expires_at > "+arg(time.Now().UTC()))
	}
	sql := `SELECT ` + cols + ` FROM forge_invites`
	if len(conds) > 0 {
		sql += " WHERE " + strings.Join(conds, " AND ")
	}
	sql += " ORDER BY id DESC"
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Non-nil empty (not nil) on zero rows, matching the memory store so
	// callers see identical List results whichever Store backs the Manager.
	out := []invite.Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// Accept marks the invite accepted at `at` if it is still pending. The
// conditional UPDATE is the single-use guard: exactly one concurrent
// accept matches the row, the rest classify their refusal.
func (s *Store) Accept(ctx context.Context, inviteID id.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forge_invites SET accepted_at = $2
		 WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2`,
		inviteID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	inv, err := s.Get(ctx, inviteID)
	if err != nil {
		return err // includes invite.ErrNotFound
	}
	switch {
	case !inv.AcceptedAt.IsZero():
		return invite.ErrAlreadyAccepted
	case !inv.RevokedAt.IsZero():
		return invite.ErrRevoked
	default:
		return invite.ErrExpired
	}
}

// Revoke marks the invite revoked at `at` unless it was accepted.
// Already-revoked is a no-op success keeping the original revoked_at.
func (s *Store) Revoke(ctx context.Context, inviteID id.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forge_invites SET revoked_at = $2
		 WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL`,
		inviteID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	inv, err := s.Get(ctx, inviteID)
	if err != nil {
		return err
	}
	if !inv.AcceptedAt.IsZero() {
		return invite.ErrAlreadyAccepted
	}
	return nil // already revoked — idempotent
}

// Rotate swaps the token hash and expiry if the invite is neither
// accepted nor revoked (expired invites rotate — that is Resend).
func (s *Store) Rotate(ctx context.Context, inviteID id.UUID, hash string, expiresAt time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forge_invites SET hash = $2, expires_at = $3
		 WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL`,
		inviteID, hash, expiresAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation on hash
		return invite.ErrDuplicate
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	inv, err := s.Get(ctx, inviteID)
	if err != nil {
		return err
	}
	switch {
	case !inv.AcceptedAt.IsZero():
		return invite.ErrAlreadyAccepted
	default:
		return invite.ErrRevoked
	}
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanInvite(r row) (invite.Invite, error) {
	var inv invite.Invite
	var acc, rv *time.Time
	err := r.Scan(&inv.ID, &inv.Hash, &inv.Email, &inv.Tenant, &inv.Role,
		&inv.CreatedAt, &inv.ExpiresAt, &acc, &rv)
	if errors.Is(err, pgx.ErrNoRows) {
		return invite.Invite{}, invite.ErrNotFound
	}
	if err != nil {
		return invite.Invite{}, err
	}
	inv.AcceptedAt = deref(acc)
	inv.RevokedAt = deref(rv)
	return inv, nil
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
