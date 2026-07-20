package pgworkflow

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

	"github.com/dmitrymomot/forge/async/workflow"
	"github.com/dmitrymomot/forge/data/postgres"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating workflow_runs, rooted so its
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

type config struct {
	table string
}

// Option configures New and PurgeTerminalBefore.
type Option func(*config)

// WithTable overrides the runs table name (default "workflow_runs"). The
// shipped migration creates the default; a custom name requires a
// caller-managed schema of the same shape.
func WithTable(name string) Option {
	return func(c *config) { c.table = name }
}

func newConfig(opts []Option) (config, error) {
	c := config{table: "workflow_runs"}
	for _, opt := range opts {
		opt(&c)
	}
	if !tableNameRe.MatchString(c.table) {
		return config{}, fmt.Errorf("pgworkflow: invalid table name %q", c.table)
	}
	// Postgres truncates identifiers past 63 bytes: two longer names sharing a
	// prefix would silently collide on one physical table.
	if len(c.table) > 63 {
		return config{}, fmt.Errorf("pgworkflow: table name %q exceeds the 63-byte identifier limit", c.table)
	}
	return c, nil
}

// Store is the durable workflow.Store over Postgres: one row per run, updated
// with optimistic locking on the version column. Apply Migrations with
// data/migration before use.
type Store struct {
	pool      *pgxpool.Pool
	createSQL string
	getSQL    string
	updateSQL string
	existsSQL string
	deleteSQL string
}

// New builds a Store over pool. Errors on a nil pool or an invalid table name.
func New(pool *pgxpool.Pool, opts ...Option) (*Store, error) {
	if pool == nil {
		return nil, errors.New("pgworkflow: New requires a pool")
	}
	c, err := newConfig(opts)
	if err != nil {
		return nil, err
	}
	return &Store{
		pool: pool,
		createSQL: fmt.Sprintf(`INSERT INTO %s (id, workflow, scope, status, error, state, step, attempt, version, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, c.table),
		getSQL: fmt.Sprintf(`SELECT id, workflow, scope, status, error, state, step, attempt, version, created_at, updated_at
			FROM %s WHERE id = $1`, c.table),
		updateSQL: fmt.Sprintf(`UPDATE %s SET status = $3, error = $4, state = $5, step = $6, attempt = $7,
			version = version + 1, updated_at = $8 WHERE id = $1 AND version = $2`, c.table),
		existsSQL: fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1)`, c.table),
		deleteSQL: fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, c.table),
	}, nil
}

// Create implements workflow.Store.
func (s *Store) Create(ctx context.Context, run workflow.Run) error {
	if run.ID == "" {
		return errors.New("pgworkflow: Create requires a non-empty run id")
	}
	_, err := s.pool.Exec(ctx, s.createSQL,
		run.ID, run.Workflow, run.Scope, string(run.Status), run.Error, run.State,
		run.Step, run.Attempt, run.Version, run.CreatedAt, run.UpdatedAt)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return workflow.ErrRunAlreadyExists
		}
		return fmt.Errorf("pgworkflow: create run: %w", err)
	}
	return nil
}

// Get implements workflow.Store.
func (s *Store) Get(ctx context.Context, id string) (workflow.Run, error) {
	var run workflow.Run
	var status string
	err := s.pool.QueryRow(ctx, s.getSQL, id).Scan(
		&run.ID, &run.Workflow, &run.Scope, &status, &run.Error, &run.State,
		&run.Step, &run.Attempt, &run.Version, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return workflow.Run{}, workflow.ErrRunNotFound
		}
		return workflow.Run{}, fmt.Errorf("pgworkflow: get run: %w", err)
	}
	run.Status = workflow.Status(status)
	run.CreatedAt = run.CreatedAt.UTC()
	run.UpdatedAt = run.UpdatedAt.UTC()
	return run, nil
}

// Update implements workflow.Store. Only the mutable columns move: workflow,
// scope, and created_at are immutable after Create.
func (s *Store) Update(ctx context.Context, run workflow.Run) error {
	tag, err := s.pool.Exec(ctx, s.updateSQL,
		run.ID, run.Version, string(run.Status), run.Error, run.State,
		run.Step, run.Attempt, run.UpdatedAt)
	if err != nil {
		return fmt.Errorf("pgworkflow: update run: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, s.existsSQL, run.ID).Scan(&exists); err != nil {
		return fmt.Errorf("pgworkflow: update run: distinguish stale from missing: %w", err)
	}
	if !exists {
		return workflow.ErrRunNotFound
	}
	return workflow.ErrStaleRun
}

// Delete implements workflow.Store.
func (s *Store) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, s.deleteSQL, id)
	if err != nil {
		return fmt.Errorf("pgworkflow: delete run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return workflow.ErrRunNotFound
	}
	return nil
}

// purgeBatch bounds one PurgeTerminalBefore statement, keeping each delete a
// short transaction with WAL and vacuum debt paid off between batches instead
// of accumulated across one multi-million-row delete (pgqueue precedent).
const purgeBatch = 5000

// PurgeTerminalBefore deletes completed and failed runs last updated strictly
// before cutoff, returning how many were removed. Terminal runs are audit
// data, not live state, so retention is the consumer's call — run this from a
// scheduled job. Deletes are batched, so it is safe on an unswept backlog.
func PurgeTerminalBefore(ctx context.Context, pool *pgxpool.Pool, cutoff time.Time, opts ...Option) (int, error) {
	if pool == nil {
		return 0, errors.New("pgworkflow: PurgeTerminalBefore requires a pool")
	}
	c, err := newConfig(opts)
	if err != nil {
		return 0, err
	}
	// ctid batching: one InitPlan picks the batch by the partial terminal
	// index, and the delete addresses rows directly — no per-batch semi-join
	// over the whole table.
	sql := fmt.Sprintf(`DELETE FROM %[1]s WHERE ctid = ANY(ARRAY(
		SELECT ctid FROM %[1]s WHERE status IN ('completed', 'failed') AND updated_at < $1 LIMIT %[2]d))`, c.table, purgeBatch)
	total := 0
	for {
		tag, err := pool.Exec(ctx, sql, cutoff)
		if err != nil {
			return total, fmt.Errorf("pgworkflow: purge terminal before: %w", err)
		}
		n := int(tag.RowsAffected())
		total += n
		// Terminate on an empty batch, not a short one: a concurrent purge
		// deleting part of this run's selected batch would otherwise make both
		// runs exit early with purgeable rows remaining.
		if n == 0 {
			return total, nil
		}
	}
}
