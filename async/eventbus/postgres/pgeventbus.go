package pgeventbus

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
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating eventbus_inbox, rooted so its
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

// Option configures NewInbox and PurgeSeenBefore.
type Option func(*config)

// WithTable overrides the inbox table name (default "eventbus_inbox"). The
// shipped migration creates the default; a custom name requires a
// caller-managed schema of the same shape.
func WithTable(name string) Option {
	return func(c *config) { c.table = name }
}

func newConfig(opts []Option) (config, error) {
	c := config{table: "eventbus_inbox"}
	for _, opt := range opts {
		opt(&c)
	}
	if !tableNameRe.MatchString(c.table) {
		return config{}, fmt.Errorf("pgeventbus: invalid table name %q", c.table)
	}
	// Postgres truncates identifiers past 63 bytes: two longer names sharing a
	// prefix would silently collide on one physical table.
	if len(c.table) > 63 {
		return config{}, fmt.Errorf("pgeventbus: table name %q exceeds the 63-byte identifier limit", c.table)
	}
	return c, nil
}

// Inbox is the transactional eventbus.Inbox for one consumer: Seen rides the
// caller's pgx.Tx, so marking an event processed commits or rolls back
// atomically with the handler's own writes. Consumers share one table keyed
// by (consumer, event id); build one Inbox per subscription with the
// subscription's name as consumer.
type Inbox struct {
	consumer string
	seenSQL  string
}

// NewInbox builds an Inbox for consumer (convention: the full subscription
// name, e.g. "user.created.send_welcome"). Errors on an empty consumer or an
// invalid table name.
func NewInbox(consumer string, opts ...Option) (*Inbox, error) {
	if consumer == "" {
		return nil, errors.New("pgeventbus: NewInbox requires a non-empty consumer")
	}
	c, err := newConfig(opts)
	if err != nil {
		return nil, err
	}
	return &Inbox{
		consumer: consumer,
		seenSQL:  fmt.Sprintf("INSERT INTO %s (consumer, event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", c.table),
	}, nil
}

// Seen implements eventbus.Inbox: it claims id for this consumer inside tx
// and reports whether it was already claimed. tx must be a pgx.Tx — the same
// transaction the handler writes in, or the claim would not roll back with a
// failed handler.
func (i *Inbox) Seen(ctx context.Context, tx any, id string) (bool, error) {
	pgtx, ok := tx.(pgx.Tx)
	if !ok {
		return false, fmt.Errorf("pgeventbus: Seen requires a pgx.Tx, got %T", tx)
	}
	if id == "" {
		return false, errors.New("pgeventbus: Seen requires a non-empty event id")
	}
	tag, err := pgtx.Exec(ctx, i.seenSQL, i.consumer, id)
	if err != nil {
		return false, fmt.Errorf("pgeventbus: seen: %w", err)
	}
	return tag.RowsAffected() == 0, nil
}

// purgeBatch bounds one PurgeSeenBefore statement, keeping each delete a
// short transaction with WAL and vacuum debt paid off between batches
// instead of accumulated across one multi-million-row delete (pgqueue
// precedent).
const purgeBatch = 5000

// PurgeSeenBefore deletes inbox rows — across all consumers — marked
// strictly before cutoff, and returns how many were removed. The inbox only
// dedups deliveries that can still arrive, so retention needs to outlive the
// worker's retry horizon, not grow forever; run this from a scheduled job.
// Deletes are batched, so it is safe on an unswept backlog.
func PurgeSeenBefore(ctx context.Context, pool *pgxpool.Pool, cutoff time.Time, opts ...Option) (int, error) {
	if pool == nil {
		return 0, errors.New("pgeventbus: PurgeSeenBefore requires a pool")
	}
	c, err := newConfig(opts)
	if err != nil {
		return 0, err
	}
	// ctid batching: one InitPlan picks the batch by the seen_at index, and
	// the delete addresses rows directly — no per-batch semi-join over the
	// whole table.
	sql := fmt.Sprintf(`DELETE FROM %[1]s WHERE ctid = ANY(ARRAY(SELECT ctid FROM %[1]s WHERE seen_at < $1 LIMIT %[2]d))`, c.table, purgeBatch)
	total := 0
	for {
		tag, err := pool.Exec(ctx, sql, cutoff)
		if err != nil {
			return total, fmt.Errorf("pgeventbus: purge seen before: %w", err)
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
