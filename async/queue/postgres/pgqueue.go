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
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating queue_jobs, rooted so its
// .sql files sit at fsys root. Apply via data/migration under its own
// version table.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

var tableNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Broker is the Postgres queue.Broker and queue.TxPusher.
type Broker struct {
	pool  *pgxpool.Pool
	table string

	insertSQL  string
	claimSQL   string
	extendSQL  string
	ackSQL     string
	nackSQL    string
	killSQL    string
	deadSQL    string
	requeueSQL string
	purgeSQL   string
	existsSQL  string
	statsSQL   string
}

// Option configures New.
type Option func(*Broker)

// WithTable overrides the table name (default "queue_jobs"). The shipped
// migration creates "queue_jobs"; custom names require a caller-managed
// schema of the same shape.
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
	const cols = "id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, last_error"
	b.insertSQL = fmt.Sprintf("INSERT INTO %s (id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')", b.table)
	b.claimSQL = fmt.Sprintf(`UPDATE %[1]s SET claimed_until = now() + $3, attempt = attempt + 1 WHERE id IN (SELECT id FROM %[1]s WHERE queue = $1 AND status = 'pending' AND run_at <= now() AND (claimed_until IS NULL OR claimed_until < now()) ORDER BY run_at, id LIMIT $2 FOR UPDATE SKIP LOCKED) RETURNING `+cols, b.table)
	b.extendSQL = fmt.Sprintf("UPDATE %s SET claimed_until = now() + $2 WHERE id = $1", b.table)
	b.ackSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1", b.table)
	b.nackSQL = fmt.Sprintf("UPDATE %s SET run_at = $2, claimed_until = NULL, last_error = $3 WHERE id = $1", b.table)
	b.killSQL = fmt.Sprintf("UPDATE %s SET status = 'dead', claimed_until = NULL, last_error = $2 WHERE id = $1", b.table)
	b.deadSQL = fmt.Sprintf("SELECT "+cols+" FROM %s WHERE queue = $1 AND status = 'dead' ORDER BY created_at, id LIMIT $2", b.table)
	b.requeueSQL = fmt.Sprintf("UPDATE %s SET status = 'pending', attempt = 0, run_at = now(), claimed_until = NULL WHERE id = $1 AND status = 'dead'", b.table)
	b.purgeSQL = fmt.Sprintf("DELETE FROM %s WHERE id = $1 AND status = 'dead'", b.table)
	b.existsSQL = fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1)", b.table)
	b.statsSQL = fmt.Sprintf("SELECT queue, status, count(*) FROM %s GROUP BY queue, status", b.table)
	return b, nil
}

func (b *Broker) Push(ctx context.Context, job queue.Job) error {
	_, err := b.pool.Exec(ctx, b.insertSQL, job.ID, job.Queue, job.Type, job.Payload, job.Scope, job.Attempt, job.MaxAttempts, job.RunAt, job.CreatedAt)
	if err != nil {
		return fmt.Errorf("pgqueue: push: %w", err)
	}
	return nil
}

// PushTx implements queue.TxPusher: the same insert inside a caller-owned
// pgx.Tx, so the job commits or rolls back with the business transaction.
func (b *Broker) PushTx(ctx context.Context, tx any, job queue.Job) error {
	pgtx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("pgqueue: push tx: expected pgx.Tx, got %T", tx)
	}
	_, err := pgtx.Exec(ctx, b.insertSQL, job.ID, job.Queue, job.Type, job.Payload, job.Scope, job.Attempt, job.MaxAttempts, job.RunAt, job.CreatedAt)
	if err != nil {
		return fmt.Errorf("pgqueue: push tx: %w", err)
	}
	return nil
}

func (b *Broker) Claim(ctx context.Context, queueName string, n int, lease time.Duration) ([]queue.Job, error) {
	rows, err := b.pool.Query(ctx, b.claimSQL, queueName, n, lease)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: claim: %w", err)
	}
	defer rows.Close()
	var jobs []queue.Job
	for rows.Next() {
		var j queue.Job
		if err := rows.Scan(&j.ID, &j.Queue, &j.Type, &j.Payload, &j.Scope, &j.Attempt, &j.MaxAttempts, &j.RunAt, &j.CreatedAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("pgqueue: claim scan: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: claim rows: %w", err)
	}
	return jobs, nil
}

func (b *Broker) Extend(ctx context.Context, id string, lease time.Duration) error {
	tag, err := b.pool.Exec(ctx, b.extendSQL, id, lease)
	if err != nil {
		return fmt.Errorf("pgqueue: extend: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
}

func (b *Broker) Ack(ctx context.Context, id string) error {
	tag, err := b.pool.Exec(ctx, b.ackSQL, id)
	if err != nil {
		return fmt.Errorf("pgqueue: ack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
}

func (b *Broker) Nack(ctx context.Context, id string, retryAt time.Time, reason string) error {
	tag, err := b.pool.Exec(ctx, b.nackSQL, id, retryAt, reason)
	if err != nil {
		return fmt.Errorf("pgqueue: nack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
}

func (b *Broker) Kill(ctx context.Context, id string, reason string) error {
	tag, err := b.pool.Exec(ctx, b.killSQL, id, reason)
	if err != nil {
		return fmt.Errorf("pgqueue: kill: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queue.ErrJobNotFound
	}
	return nil
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

func (b *Broker) Requeue(ctx context.Context, id string) error {
	tag, err := b.pool.Exec(ctx, b.requeueSQL, id)
	if err != nil {
		return fmt.Errorf("pgqueue: requeue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, id)
	}
	return nil
}

func (b *Broker) Purge(ctx context.Context, id string) error {
	tag, err := b.pool.Exec(ctx, b.purgeSQL, id)
	if err != nil {
		return fmt.Errorf("pgqueue: purge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return b.notDeadOrMissing(ctx, id)
	}
	return nil
}

func (b *Broker) notDeadOrMissing(ctx context.Context, id string) error {
	var exists bool
	if err := b.pool.QueryRow(ctx, b.existsSQL, id).Scan(&exists); err != nil {
		return fmt.Errorf("pgqueue: exists: %w", err)
	}
	if exists {
		return queue.ErrNotDead
	}
	return queue.ErrJobNotFound
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	rows, err := b.pool.Query(ctx, b.statsSQL)
	if err != nil {
		return nil, fmt.Errorf("pgqueue: stats: %w", err)
	}
	defer rows.Close()
	st := make(queue.Stats)
	for rows.Next() {
		var q, status string
		var n int64
		if err := rows.Scan(&q, &status, &n); err != nil {
			return nil, fmt.Errorf("pgqueue: stats scan: %w", err)
		}
		qs := st[q]
		if status == "dead" {
			qs.Dead = int(n)
		} else {
			qs.Pending = int(n)
		}
		st[q] = qs
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgqueue: stats rows: %w", err)
	}
	return st, nil
}
