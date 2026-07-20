package pgscheduler

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/async/scheduler"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating scheduler_claims, rooted so
// its .sql files sit at fsys root. Apply via data/migration under its own
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

type config struct {
	table string
}

// Option configures NewStore.
type Option func(*config)

// WithTable overrides the claim table name (default "scheduler_claims"). The
// shipped migration creates the default; a custom name requires a
// caller-managed schema of the same shape.
func WithTable(name string) Option {
	return func(c *config) { c.table = name }
}

func newConfig(opts []Option) (config, error) {
	c := config{table: "scheduler_claims"}
	for _, opt := range opts {
		opt(&c)
	}
	if !tableNameRe.MatchString(c.table) {
		return config{}, fmt.Errorf("pgscheduler: invalid table name %q", c.table)
	}
	// Postgres truncates identifiers past 63 bytes: two longer names sharing a
	// prefix would silently collide on one physical table.
	if len(c.table) > 63 {
		return config{}, fmt.Errorf("pgscheduler: table name %q exceeds the 63-byte identifier limit", c.table)
	}
	return c, nil
}

// Store is the Postgres scheduler.Store: the fleet-wide claim table whose
// unique (name, scheduled_for) insert race decides which instance enqueues
// each tick.
type Store struct {
	pool       *pgxpool.Pool
	claimSQL   string
	releaseSQL string
	purgeSQL   string
}

// NewStore builds a Store over pool. Errors on a nil pool or an invalid
// table name.
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error) {
	if pool == nil {
		return nil, errors.New("pgscheduler: NewStore requires a pool")
	}
	c, err := newConfig(opts)
	if err != nil {
		return nil, err
	}
	return &Store{
		pool:     pool,
		claimSQL: fmt.Sprintf("INSERT INTO %s (name, scheduled_for) VALUES ($1, $2) ON CONFLICT DO NOTHING", c.table),
		// purgeBatch bounds one delete statement via ctid batching: one InitPlan
		// picks the batch by the scheduled_for index, and the delete addresses
		// rows directly (pgeventbus precedent).
		purgeSQL:   fmt.Sprintf(`DELETE FROM %[1]s WHERE ctid = ANY(ARRAY(SELECT ctid FROM %[1]s WHERE scheduled_for < $1 LIMIT %[2]d))`, c.table, purgeBatch),
		releaseSQL: fmt.Sprintf("DELETE FROM %s WHERE name = $1 AND scheduled_for = $2", c.table),
	}, nil
}

// Claim implements scheduler.Store: the insert race. Exactly one Claim per
// (name, scheduled_for) succeeds; the rest get scheduler.ErrAlreadyClaimed.
func (s *Store) Claim(ctx context.Context, name string, scheduledFor time.Time) error {
	if name == "" {
		return errors.New("pgscheduler: Claim requires a non-empty job name")
	}
	tag, err := s.pool.Exec(ctx, s.claimSQL, name, scheduledFor)
	if err != nil {
		return fmt.Errorf("pgscheduler: claim: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return scheduler.ErrAlreadyClaimed
	}
	return nil
}

// Release implements scheduler.Store: it deletes a claim after a failed
// enqueue so the tick can be claimed again. Releasing an absent claim is a
// no-op.
func (s *Store) Release(ctx context.Context, name string, scheduledFor time.Time) error {
	if _, err := s.pool.Exec(ctx, s.releaseSQL, name, scheduledFor); err != nil {
		return fmt.Errorf("pgscheduler: release: %w", err)
	}
	return nil
}

// purgeBatch keeps each PurgeBefore delete a short transaction with WAL and
// vacuum debt paid off between batches instead of accumulated across one
// multi-million-row delete (pgqueue precedent).
const purgeBatch = 5000

// PurgeBefore implements scheduler.Store: it deletes claims scheduled
// strictly before cutoff and returns how many were removed. Deletes are
// batched, so it is safe on an unswept backlog.
func (s *Store) PurgeBefore(ctx context.Context, cutoff time.Time) (int, error) {
	total := 0
	for {
		tag, err := s.pool.Exec(ctx, s.purgeSQL, cutoff)
		if err != nil {
			return total, fmt.Errorf("pgscheduler: purge before: %w", err)
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
