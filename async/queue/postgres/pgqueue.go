package pgqueue

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

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating queue_jobs and
// queue_jobs_dead, rooted so its .sql files sit at fsys root. Apply via
// data/migration under its own version table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

var tableNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// statsCap bounds per-queue stats counting: counts are exact up to the cap;
// beyond it QueueStats reports the cap with the Capped flag set. Keeps Stats
// O(cap) via bounded index-only scans instead of O(table).
const statsCap = 10000

// Broker is the Postgres queue.Broker and queue.TxPusher.
type Broker struct {
	pool      *pgxpool.Pool
	table     string
	deadTable string

	insertSQL       string
	claimSQL        string
	extendSQL       string
	ackSQL          string
	nackSQL         string
	killSQL         string
	deadSQL         string
	requeueSQL      string
	purgeSQL        string
	purgeBeforeSQL  string
	existsSQL       string
	statsPendingSQL string
	statsDeadSQL    string
}

// Option configures New.
type Option func(*Broker)

// WithTable overrides the hot table name (default "queue_jobs"); the
// dead-letter table is always derived as "<table>_dead". The shipped migration
// creates the default pair; custom names require a caller-managed schema of
// the same shape.
func WithTable(name string) Option {
	return func(b *Broker) { b.table = name }
}

// New builds a Broker over pool.
func New(pool *pgxpool.Pool, opts ...Option) (*Broker, error) {
	b := &Broker{pool: pool, table: "queue_jobs"}
	for _, opt := range opts {
		opt(b)
	}
	if pool == nil {
		return nil, errors.New("pgqueue: nil pool")
	}
	if !tableNameRe.MatchString(b.table) {
		return nil, fmt.Errorf("pgqueue: invalid table name %q", b.table)
	}
	b.deadTable = b.table + "_dead"

	const liveCols = "id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, last_error"
	b.insertSQL = fmt.Sprintf(`INSERT INTO %s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at)
SELECT u.id, u.queue, u.type, u.payload::json, u.scope, u.attempt, u.max_attempts, u.run_at, u.created_at
FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[], $6::int[], $7::int[], $8::timestamptz[], $9::timestamptz[])
AS u(id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at)`, b.table)
	b.claimSQL = fmt.Sprintf(`WITH picked AS (
SELECT id FROM %[1]s
WHERE queue = $1 AND run_at <= now() AND (claimed_until IS NULL OR claimed_until < now())
ORDER BY run_at, id LIMIT $2
FOR UPDATE SKIP LOCKED
), claimed AS (
UPDATE %[1]s j SET claimed_until = now() + $3, claim_token = $4, attempt = j.attempt + 1
FROM picked WHERE j.id = picked.id
RETURNING j.id, j.queue, j.type, j.payload, j.scope, j.attempt, j.max_attempts, j.run_at, j.created_at, j.last_error
)
SELECT `+liveCols+` FROM claimed ORDER BY run_at, id`, b.table)
	b.extendSQL = fmt.Sprintf("UPDATE %s SET claimed_until = now() + $3 WHERE id = $1 AND claim_token = $2", b.table)
	b.ackSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1 AND claim_token = $2", b.table)
	b.nackSQL = fmt.Sprintf("UPDATE %s SET run_at = $3, claimed_until = NULL, claim_token = NULL, last_error = $4 WHERE id = $1 AND claim_token = $2", b.table)
	b.killSQL = fmt.Sprintf(`WITH d AS (
DELETE FROM %[1]s WHERE id = $1 AND claim_token = $2
RETURNING id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at
)
INSERT INTO %[2]s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, died_at, last_error)
SELECT id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, now(), $3 FROM d`, b.table, b.deadTable)
	b.deadSQL = fmt.Sprintf("SELECT "+liveCols+" FROM %s WHERE queue = $1 ORDER BY died_at, id LIMIT $2", b.deadTable)
	b.requeueSQL = fmt.Sprintf(`WITH d AS (
DELETE FROM %[2]s WHERE id = $1
RETURNING id, queue, type, payload, scope, max_attempts, created_at, last_error
)
INSERT INTO %[1]s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, last_error)
SELECT id, queue, type, payload, scope, 0, max_attempts, now(), created_at, last_error FROM d`, b.table, b.deadTable)
	b.purgeSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1", b.deadTable)
	b.purgeBeforeSQL = fmt.Sprintf("DELETE FROM %s WHERE died_at < $1", b.deadTable)
	b.existsSQL = fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1)", b.table)
	b.statsPendingSQL = fmt.Sprintf(`SELECT q.queue, c.n FROM (SELECT DISTINCT queue FROM %[1]s) q
CROSS JOIN LATERAL (SELECT count(*) AS n FROM (SELECT 1 FROM %[1]s j WHERE j.queue = q.queue LIMIT %[2]d) t) c`, b.table, statsCap+1)
	b.statsDeadSQL = fmt.Sprintf(`SELECT q.queue, c.n FROM (SELECT DISTINCT queue FROM %[1]s) q
CROSS JOIN LATERAL (SELECT count(*) AS n FROM (SELECT 1 FROM %[1]s j WHERE j.queue = q.queue LIMIT %[2]d) t) c`, b.deadTable, statsCap+1)
	return b, nil
}

// pushArgs flattens jobs into the parallel arrays the unnest insert binds.
func pushArgs(jobs []queue.Job) []any {
	n := len(jobs)
	ids := make([]string, n)
	queues := make([]string, n)
	types := make([]string, n)
	payloads := make([]string, n)
	scopes := make([]string, n)
	attempts := make([]int32, n)
	maxAttempts := make([]int32, n)
	runAts := make([]time.Time, n)
	createdAts := make([]time.Time, n)
	for i, j := range jobs {
		ids[i] = j.ID
		queues[i] = j.Queue
		types[i] = j.Type
		payloads[i] = string(j.Payload)
		scopes[i] = j.Scope
		attempts[i] = int32(j.Attempt)
		maxAttempts[i] = int32(j.MaxAttempts)
		runAts[i] = j.RunAt
		createdAts[i] = j.CreatedAt
	}
	return []any{ids, queues, types, payloads, scopes, attempts, maxAttempts, runAts, createdAts}
}

// copyFromThreshold gates Push/PushTx's insert strategy: below it, the single
// unnest-array INSERT (one bind, one round trip) is cheaper than paying for
// COPY protocol setup; at or above it, streaming rows via pgx.CopyFrom wins.
// Measured on Apple M3 Max / postgres:18-alpine (10k-row batch): unnest
// ~41.3ms vs CopyFrom ~24.3ms (41% faster); crossover was around 1000 rows,
// so 2000 leaves margin. See
// docs/superpowers/specs/2026-07-16-queue-bench-after.txt for the full table.
const copyFromThreshold = 2000

// copyFromCols mirrors the unnest insert's column list (minus the
// last_error/claimed_until/claim_token columns, which default on insert).
var copyFromCols = []string{"id", "queue", "type", "payload", "scope", "attempt", "max_attempts", "run_at", "created_at"}

// copier is satisfied by both *pgxpool.Pool and pgx.Tx, letting pushCopyFrom
// serve Push and PushTx alike.
type copier interface {
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

func (b *Broker) pushCopyFrom(ctx context.Context, cp copier, jobs []queue.Job) error {
	src := pgx.CopyFromSlice(len(jobs), func(i int) ([]any, error) {
		j := jobs[i]
		return []any{j.ID, j.Queue, j.Type, string(j.Payload), j.Scope, int32(j.Attempt), int32(j.MaxAttempts), j.RunAt, j.CreatedAt}, nil
	})
	_, err := cp.CopyFrom(ctx, pgx.Identifier{b.table}, copyFromCols, src)
	return err
}

func (b *Broker) Push(ctx context.Context, jobs ...queue.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	if len(jobs) >= copyFromThreshold {
		if err := b.pushCopyFrom(ctx, b.pool, jobs); err != nil {
			return fmt.Errorf("pgqueue: push: %w", err)
		}
		return nil
	}
	if _, err := b.pool.Exec(ctx, b.insertSQL, pushArgs(jobs)...); err != nil {
		return fmt.Errorf("pgqueue: push: %w", err)
	}
	return nil
}

// PushTx implements queue.TxPusher: the same batch insert inside a
// caller-owned pgx.Tx, so the jobs commit or roll back with the business
// transaction.
func (b *Broker) PushTx(ctx context.Context, tx any, jobs ...queue.Job) error {
	pgtx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("pgqueue: push tx: expected pgx.Tx, got %T", tx)
	}
	if len(jobs) == 0 {
		return nil
	}
	if len(jobs) >= copyFromThreshold {
		if err := b.pushCopyFrom(ctx, pgtx, jobs); err != nil {
			return fmt.Errorf("pgqueue: push tx: %w", err)
		}
		return nil
	}
	if _, err := pgtx.Exec(ctx, b.insertSQL, pushArgs(jobs)...); err != nil {
		return fmt.Errorf("pgqueue: push tx: %w", err)
	}
	return nil
}

func (b *Broker) Claim(ctx context.Context, queueName string, n int, lease time.Duration) ([]queue.ClaimedJob, error) {
	token := id.NewUUID().String()
	rows, err := b.pool.Query(ctx, b.claimSQL, queueName, n, lease, token)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: claim: %w", err)
	}
	defer rows.Close()
	var jobs []queue.ClaimedJob
	for rows.Next() {
		var j queue.Job
		if err := rows.Scan(&j.ID, &j.Queue, &j.Type, &j.Payload, &j.Scope, &j.Attempt, &j.MaxAttempts, &j.RunAt, &j.CreatedAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("pgqueue: claim scan: %w", err)
		}
		jobs = append(jobs, queue.ClaimedJob{Job: j, Token: token})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: claim rows: %w", err)
	}
	return jobs, nil
}

// fencedExec runs a token-guarded statement; zero affected rows means the
// token no longer owns the job.
func (b *Broker) fencedExec(ctx context.Context, op, sql string, args ...any) error {
	tag, err := b.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("pgqueue: %s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Extend(ctx context.Context, jobID, token string, lease time.Duration) error {
	return b.fencedExec(ctx, "extend", b.extendSQL, jobID, token, lease)
}

func (b *Broker) Ack(ctx context.Context, jobID, token string) error {
	return b.fencedExec(ctx, "ack", b.ackSQL, jobID, token)
}

func (b *Broker) Nack(ctx context.Context, jobID, token string, retryAt time.Time, reason string) error {
	return b.fencedExec(ctx, "nack", b.nackSQL, jobID, token, retryAt, reason)
}

func (b *Broker) Kill(ctx context.Context, jobID, token string, reason string) error {
	return b.fencedExec(ctx, "kill", b.killSQL, jobID, token, reason)
}

func (b *Broker) ListDead(ctx context.Context, queueName string, limit int) ([]queue.Job, error) {
	rows, err := b.pool.Query(ctx, b.deadSQL, queueName, limit)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: list dead: %w", err)
	}
	defer rows.Close()
	var jobs []queue.Job
	for rows.Next() {
		var j queue.Job
		if err := rows.Scan(&j.ID, &j.Queue, &j.Type, &j.Payload, &j.Scope, &j.Attempt, &j.MaxAttempts, &j.RunAt, &j.CreatedAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("pgqueue: list dead scan: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: list dead rows: %w", err)
	}
	return jobs, nil
}

func (b *Broker) Requeue(ctx context.Context, jobID string) error {
	tag, err := b.pool.Exec(ctx, b.requeueSQL, jobID)
	if err != nil {
		return fmt.Errorf("pgqueue: requeue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, jobID)
	}
	return nil
}

func (b *Broker) Purge(ctx context.Context, jobID string) error {
	tag, err := b.pool.Exec(ctx, b.purgeSQL, jobID)
	if err != nil {
		return fmt.Errorf("pgqueue: purge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, jobID)
	}
	return nil
}

func (b *Broker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := b.pool.Exec(ctx, b.purgeBeforeSQL, cutoff)
	if err != nil {
		return 0, fmt.Errorf("pgqueue: purge dead before: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (b *Broker) notDeadOrMissing(ctx context.Context, jobID string) error {
	var exists bool
	if err := b.pool.QueryRow(ctx, b.existsSQL, jobID).Scan(&exists); err != nil {
		return fmt.Errorf("pgqueue: exists: %w", err)
	}
	if exists {
		return queue.ErrNotDead
	}
	return queue.ErrJobNotFound
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	st := make(queue.Stats)
	if err := b.statsInto(ctx, b.statsPendingSQL, st, false); err != nil {
		return nil, err
	}
	if err := b.statsInto(ctx, b.statsDeadSQL, st, true); err != nil {
		return nil, err
	}
	return st, nil
}

// statsInto merges one bounded count query into st. Counts run with LIMIT
// statsCap+1: a full statsCap+1 result means "more than the cap", reported as
// the cap with the Capped flag set.
func (b *Broker) statsInto(ctx context.Context, sql string, st queue.Stats, dead bool) error {
	rows, err := b.pool.Query(ctx, sql)
	if err != nil {
		return fmt.Errorf("pgqueue: stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var q string
		var n int64
		if err := rows.Scan(&q, &n); err != nil {
			return fmt.Errorf("pgqueue: stats scan: %w", err)
		}
		capped := n > statsCap
		if capped {
			n = statsCap
		}
		qs := st[q]
		if dead {
			qs.Dead, qs.DeadCapped = int(n), capped
		} else {
			qs.Pending, qs.PendingCapped = int(n), capped
		}
		st[q] = qs
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pgqueue: stats rows: %w", err)
	}
	return nil
}
