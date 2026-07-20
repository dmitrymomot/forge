// Package pgoutbox is the Postgres outbox.Store: intent rows in an
// outbox_jobs table inside the business database, so Add rides the caller's
// pgx.Tx and the rows commit or roll back with the business writes. Claim
// uses FOR UPDATE SKIP LOCKED, so any number of relay instances share one
// table without coordination.
package pgoutbox

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/async/outbox"
	"github.com/dmitrymomot/forge/async/queue"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating outbox_jobs, rooted so its
// .sql files sit at fsys root. Apply via data/migration under its own version
// table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

var tableNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// statsCap bounds Stats counting: Pending is exact up to the cap; beyond it
// Stats reports the cap with PendingCapped set (pgqueue precedent). Keeps
// Stats O(cap) via a bounded scan instead of O(table).
const statsCap = 10000

// Store is the Postgres outbox.Store.
type Store struct {
	pool *pgxpool.Pool

	table string

	insertSQL      string
	claimSQL       string
	deleteSQL      string
	failSQL        string
	statsCountSQL  string
	statsOldestSQL string
}

// Option configures New.
type Option func(*Store)

// WithTable overrides the table name (default "outbox_jobs"). The shipped
// migration creates the default; a custom name requires a caller-managed
// schema of the same shape.
func WithTable(name string) Option {
	return func(s *Store) { s.table = name }
}

// New builds a Store over pool. The pool is used by the relay-facing methods
// (Claim, Delete, Fail, Stats); Add runs on the caller's transaction.
func New(pool *pgxpool.Pool, opts ...Option) (*Store, error) {
	s := &Store{pool: pool, table: "outbox_jobs"}
	for _, opt := range opts {
		opt(s)
	}
	if pool == nil {
		return nil, errors.New("pgoutbox: nil pool")
	}
	if !tableNameRe.MatchString(s.table) {
		return nil, fmt.Errorf("pgoutbox: invalid table name %q", s.table)
	}
	// Postgres truncates identifiers past 63 bytes: two longer names sharing a
	// prefix would silently collide on one physical table.
	if len(s.table) > 63 {
		return nil, fmt.Errorf("pgoutbox: table name %q exceeds the 63-byte identifier limit", s.table)
	}

	// available_at is stamped by the database clock, not the client's
	// CreatedAt: Claim compares against now(), and a client clock running
	// ahead of Postgres would otherwise delay every row by the skew.
	s.insertSQL = fmt.Sprintf(`INSERT INTO %s (id, queue, type, payload, scope, max_attempts, run_at, created_at, available_at)
SELECT u.id, u.queue, u.type, u.payload::json, u.scope, u.max_attempts, u.run_at, u.created_at, now()
FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[], $6::int[], $7::timestamptz[], $8::timestamptz[])
AS u(id, queue, type, payload, scope, max_attempts, run_at, created_at)`, s.table)
	s.claimSQL = fmt.Sprintf(`WITH picked AS (
SELECT id FROM %[1]s
WHERE available_at <= now()
ORDER BY created_at, id LIMIT $1
FOR UPDATE SKIP LOCKED
), claimed AS (
UPDATE %[1]s o SET available_at = now() + $2, attempts = o.attempts + 1
FROM picked WHERE o.id = picked.id
RETURNING o.id, o.queue, o.type, o.payload, o.scope, o.max_attempts, o.run_at, o.created_at, o.attempts, o.last_error
)
SELECT id, queue, type, payload, scope, max_attempts, run_at, created_at, attempts, last_error
FROM claimed ORDER BY created_at, id`, s.table)
	s.deleteSQL = fmt.Sprintf("DELETE FROM %s WHERE id = ANY($1::uuid[])", s.table)
	s.failSQL = fmt.Sprintf("UPDATE %s SET available_at = $2, last_error = $3 WHERE id = $1", s.table)
	s.statsCountSQL = fmt.Sprintf("SELECT count(*) FROM (SELECT 1 FROM %s LIMIT %d) t", s.table, statsCap+1)
	s.statsOldestSQL = fmt.Sprintf("SELECT min(created_at) FROM %s", s.table)
	return s, nil
}

// addArgs flattens jobs into the parallel arrays the unnest insert binds.
func addArgs(jobs []queue.Job) []any {
	n := len(jobs)
	ids := make([]string, n)
	queues := make([]string, n)
	types := make([]string, n)
	payloads := make([]string, n)
	scopes := make([]string, n)
	maxAttempts := make([]int32, n)
	runAts := make([]time.Time, n)
	createdAts := make([]time.Time, n)
	for i, j := range jobs {
		ids[i] = j.ID
		queues[i] = j.Queue
		types[i] = j.Type
		payloads[i] = string(j.Payload)
		scopes[i] = j.Scope
		maxAttempts[i] = int32(j.MaxAttempts)
		runAts[i] = j.RunAt
		createdAts[i] = j.CreatedAt
	}
	return []any{ids, queues, types, payloads, scopes, maxAttempts, runAts, createdAts}
}

// Add implements outbox.Store: one batch insert on the caller's pgx.Tx, so
// the intent rows commit or roll back with the business transaction.
func (s *Store) Add(ctx context.Context, tx any, jobs ...queue.Job) error {
	pgtx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("pgoutbox: add: expected pgx.Tx, got %T", tx)
	}
	if len(jobs) == 0 {
		return nil
	}
	if _, err := pgtx.Exec(ctx, s.insertSQL, addArgs(jobs)...); err != nil {
		return fmt.Errorf("pgoutbox: add: %w", err)
	}
	return nil
}

// Claim implements outbox.Store.
func (s *Store) Claim(ctx context.Context, n int, lease time.Duration) ([]outbox.Entry, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, s.claimSQL, n, lease)
	if err != nil {
		return nil, fmt.Errorf("pgoutbox: claim: %w", err)
	}
	defer rows.Close()
	var entries []outbox.Entry
	for rows.Next() {
		var e outbox.Entry
		if err := rows.Scan(&e.Job.ID, &e.Job.Queue, &e.Job.Type, &e.Job.Payload, &e.Job.Scope,
			&e.Job.MaxAttempts, &e.Job.RunAt, &e.Job.CreatedAt, &e.Attempts, &e.LastError); err != nil {
			return nil, fmt.Errorf("pgoutbox: claim scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgoutbox: claim: %w", err)
	}
	return entries, nil
}

// Delete implements outbox.Store. Unknown ids are ignored.
func (s *Store) Delete(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := s.pool.Exec(ctx, s.deleteSQL, ids); err != nil {
		return fmt.Errorf("pgoutbox: delete: %w", err)
	}
	return nil
}

// Fail implements outbox.Store. Unknown ids are ignored.
func (s *Store) Fail(ctx context.Context, id string, retryAt time.Time, reason string) error {
	if _, err := s.pool.Exec(ctx, s.failSQL, id, retryAt, reason); err != nil {
		return fmt.Errorf("pgoutbox: fail: %w", err)
	}
	return nil
}

// Stats implements outbox.Store. Pending is capped at 10000 (PendingCapped
// reports the cap was hit); Oldest is exact via the claim index.
func (s *Store) Stats(ctx context.Context) (outbox.Stats, error) {
	var st outbox.Stats
	if err := s.pool.QueryRow(ctx, s.statsCountSQL).Scan(&st.Pending); err != nil {
		return outbox.Stats{}, fmt.Errorf("pgoutbox: stats count: %w", err)
	}
	if st.Pending > statsCap {
		st.Pending = statsCap
		st.PendingCapped = true
	}
	var oldest *time.Time
	if err := s.pool.QueryRow(ctx, s.statsOldestSQL).Scan(&oldest); err != nil {
		return outbox.Stats{}, fmt.Errorf("pgoutbox: stats oldest: %w", err)
	}
	if oldest != nil {
		st.Oldest = *oldest
	}
	return st, nil
}
