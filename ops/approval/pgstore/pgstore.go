package pgstore

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_approval_requests,
// rooted so its .sql files sit at fsys root (data/migration.New globs
// fsys's root, not subdirectories). Apply it under its own version table
// (migration.WithTable("forge_approval_schema")) — a colliding group name
// silently skips.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of approval.Store. The pool's
// lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ approval.Store = (*Store)(nil)

// New builds a Postgres approval Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, kind, tenant, requester, reason, status, version, payload, decisions, meta, claimed_by, created_at, expires_at, claimed_at, decided_at`

const createSQL = `
INSERT INTO forge_approval_requests (` + cols + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

// Create inserts r. A colliding id yields approval.ErrDuplicate.
func (s *Store) Create(ctx context.Context, r approval.Request) error {
	payload, decisions, meta, err := encodeState(r)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, createSQL,
		r.ID, r.Kind, r.Tenant, r.Requester, r.Reason, int16(r.Status), r.Version,
		payload, decisions, meta, r.ClaimedBy,
		r.CreatedAt, nullTime(r.ExpiresAt), nullTime(r.ClaimedAt), nullTime(r.DecidedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return approval.ErrDuplicate
	}
	return err
}

// Get returns the request for reqID, or approval.ErrNotFound.
func (s *Store) Get(ctx context.Context, reqID id.UUID) (approval.Request, error) {
	return scanRequest(s.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM forge_approval_requests WHERE id = $1`, reqID))
}

const updateSQL = `
UPDATE forge_approval_requests
SET kind = $2, tenant = $3, requester = $4, reason = $5, status = $6,
    version = version + 1, payload = $7, decisions = $8, meta = $9,
    claimed_by = $10, created_at = $11, expires_at = $12, claimed_at = $13,
    decided_at = $14
WHERE id = $1 AND version = $15`

// Update persists r only when the stored version matches expect. Zero rows
// affected means either the id is gone or another writer moved the version;
// a follow-up existence check tells those apart, because the Manager
// retries one and gives up on the other.
func (s *Store) Update(ctx context.Context, r approval.Request, expect int64) error {
	payload, decisions, meta, err := encodeState(r)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, updateSQL,
		r.ID, r.Kind, r.Tenant, r.Requester, r.Reason, int16(r.Status),
		payload, decisions, meta, r.ClaimedBy,
		r.CreatedAt, nullTime(r.ExpiresAt), nullTime(r.ClaimedAt), nullTime(r.DecidedAt),
		expect)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM forge_approval_requests WHERE id = $1)`, r.ID,
	).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return approval.ErrNotFound
	}
	return approval.ErrConflict
}

const listSQL = `
SELECT ` + cols + ` FROM forge_approval_requests
WHERE ($1 = '' OR tenant = $1)
  AND ($2 = '' OR kind = $2)
  AND ($3 = '' OR requester = $3)
  AND (cardinality($4::smallint[]) = 0 OR status = ANY($4))
  AND ($5::timestamptz IS NULL OR (expires_at IS NOT NULL AND expires_at < $5))
ORDER BY id DESC
LIMIT $6`

// List returns requests matching f, newest first (UUIDv7 ids are
// time-ordered, so id DESC is creation order). A zero f.Limit defaults to
// approval.DefaultListLimit, matching the memory store.
func (s *Store) List(ctx context.Context, f approval.Filter) ([]approval.Request, error) {
	statuses := make([]int16, 0, len(f.Statuses))
	for _, st := range f.Statuses {
		statuses = append(statuses, int16(st))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = approval.DefaultListLimit
	}
	rows, err := s.pool.Query(ctx, listSQL,
		f.Tenant, f.Kind, f.Requester, statuses, nullTime(f.ExpiresBefore), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Non-nil empty on zero rows, matching the memory store.
	out := []approval.Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanRequest(rw row) (approval.Request, error) {
	var (
		r         approval.Request
		status    int16
		payload   []byte
		decisions []byte
		exp       *time.Time
		claimed   *time.Time
		decided   *time.Time
	)
	err := rw.Scan(&r.ID, &r.Kind, &r.Tenant, &r.Requester, &r.Reason, &status,
		&r.Version, &payload, &decisions, &r.Meta, &r.ClaimedBy,
		&r.CreatedAt, &exp, &claimed, &decided)
	if errors.Is(err, pgx.ErrNoRows) {
		return approval.Request{}, approval.ErrNotFound
	}
	if err != nil {
		return approval.Request{}, err
	}
	r.Status = approval.Status(status)
	r.Payload = json.RawMessage(payload)
	if err := json.Unmarshal(decisions, &r.Decisions); err != nil {
		return approval.Request{}, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	if exp != nil {
		r.ExpiresAt = exp.UTC()
	}
	if claimed != nil {
		r.ClaimedAt = claimed.UTC()
	}
	if decided != nil {
		r.DecidedAt = decided.UTC()
	}
	return r, nil
}

// encodeState prepares the three JSON columns, normalizing nil Decisions to
// an empty array and nil Meta to an empty object so a reader never has to
// special-case null. A nil Payload becomes JSON null: the column is NOT
// NULL, and the memory store accepts a nil payload, so rejecting it here
// would make the two stores diverge on the same input.
func encodeState(r approval.Request) (payload, decisions []byte, meta map[string]string, err error) {
	payload = []byte(r.Payload)
	if len(payload) == 0 {
		payload = []byte("null")
	}
	d := r.Decisions
	if d == nil {
		d = []approval.Decision{}
	}
	decisions, err = json.Marshal(d)
	if err != nil {
		return nil, nil, nil, err
	}
	meta = r.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	return payload, decisions, meta, nil
}

// nullTime maps a zero time to SQL NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
