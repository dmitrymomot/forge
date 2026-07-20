package pgsink

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_audit_events, rooted
// so its .sql files sit at fsys root (data/migration.New globs fsys's
// root, not subdirectories). Apply via data/migration under its own
// version table ("forge_auditlog_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// ErrInvalidCursor rejects List cursors that are not an event id from a
// previous page.
var ErrInvalidCursor = errors.New("pgsink: invalid cursor")

// Sink persists audit events to Postgres and serves the read side of the
// trail: tenant-isolated keyset-paginated List and the chain Verify pass.
// It implements auditlog.Sink and auditlog.ChainHead. The table is
// append-only — no update or delete path exists. The pool's lifecycle is
// the caller's.
type Sink struct {
	pool  *pgxpool.Pool
	scope func(context.Context) (string, error)
}

var (
	_ auditlog.Sink      = (*Sink)(nil)
	_ auditlog.ChainHead = (*Sink)(nil)
)

// New builds a Postgres Sink. Apply Migrations first. It panics on a nil
// pool — a wiring bug caught at startup.
func New(pool *pgxpool.Pool, opts ...Option) *Sink {
	if pool == nil {
		panic("pgsink: nil pool")
	}
	s := &Sink{pool: pool}
	for _, o := range opts {
		o(s)
	}
	return s
}

const cols = `id, tenant, actor, action, resource, outcome, meta, prev_hash, hash, occurred_at`

// Write inserts e.
func (s *Sink) Write(ctx context.Context, e auditlog.Event) error {
	meta := e.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO forge_audit_events (`+cols+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.ID, e.Tenant, e.Actor, e.Action, e.Resource, string(e.Outcome), meta,
		e.PrevHash, e.Hash, e.Time)
	return err
}

// ChainHead returns the hash of the newest event in stream, or "" for an
// empty stream, letting a chained Recorder resume across restarts.
func (s *Sink) ChainHead(ctx context.Context, stream string) (string, error) {
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT hash FROM forge_audit_events WHERE tenant = $1 ORDER BY id DESC LIMIT 1`,
		stream).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

// Filter narrows List. Zero-value fields do not filter; a zero Limit
// defaults to 50 (capped at 500). Cursor is the NextCursor of the
// previous page.
type Filter struct {
	From     time.Time
	To       time.Time
	Tenant   string
	Actor    string
	Action   string
	Resource string
	Outcome  auditlog.Outcome
	Cursor   string
	Limit    int
}

// List returns events matching f, newest first, plus the cursor of the
// next page ("" on the last page). Under WithScope the hook's tenant
// confines the query — a hook error or empty tenant fails closed with
// auditlog.ErrScope, and a conflicting f.Tenant fails with
// auditlog.ErrTenantMismatch. Unscoped, f.Tenant filters explicitly and
// "" spans all tenants (the single-tenant case).
func (s *Sink) List(ctx context.Context, f Filter) ([]auditlog.Event, string, error) {
	tenant, err := s.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, "", err
	}
	limit := f.Limit
	switch {
	case limit <= 0:
		limit = 50
	case limit > 500:
		limit = 500
	}
	var cursor any
	if f.Cursor != "" {
		u, err := id.ParseUUID(f.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %w", ErrInvalidCursor, err)
		}
		cursor = u
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM forge_audit_events
		WHERE ($1 = '' OR tenant = $1)
		  AND ($2 = '' OR actor = $2)
		  AND ($3 = '' OR action = $3)
		  AND ($4 = '' OR resource = $4)
		  AND ($5 = '' OR outcome = $5)
		  AND ($6::timestamptz IS NULL OR occurred_at >= $6)
		  AND ($7::timestamptz IS NULL OR occurred_at <= $7)
		  AND ($8::uuid IS NULL OR id < $8)
		ORDER BY id DESC
		LIMIT $9`,
		tenant, f.Actor, f.Action, f.Resource, string(f.Outcome),
		nullTime(f.From), nullTime(f.To), cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	events, err := scanEvents(rows, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(events) > limit {
		events = events[:limit]
		next = events[limit-1].ID.String()
	}
	return events, next, nil
}

// verifyBatch is the page size of Verify's ascending scan.
const verifyBatch = 500

// Verify walks stream's events in id-ascending (chain) order and checks
// the hash chain from its genesis, returning the number of events
// verified. A tampered, deleted, or reordered event fails with
// auditlog.ErrChainBroken naming the first bad event. Under WithScope the
// hook's tenant selects the stream, failing closed like List.
func (s *Sink) Verify(ctx context.Context, stream string) (int, error) {
	tenant, err := s.scoped(ctx, stream)
	if err != nil {
		return 0, err
	}
	var (
		prev  string
		after any
		count int
	)
	for {
		rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM forge_audit_events
			WHERE tenant = $1 AND ($2::uuid IS NULL OR id > $2)
			ORDER BY id ASC
			LIMIT $3`, tenant, after, verifyBatch)
		if err != nil {
			return count, err
		}
		events, err := scanEvents(rows, verifyBatch)
		if err != nil {
			return count, err
		}
		if len(events) == 0 {
			return count, nil
		}
		if prev, err = auditlog.VerifyChain(prev, events); err != nil {
			return count, err
		}
		count += len(events)
		after = events[len(events)-1].ID
		if len(events) < verifyBatch {
			return count, nil
		}
	}
}

// scoped resolves the effective tenant: the WithScope hook when
// configured (fail-closed, must agree with any explicit tenant), the
// explicit value otherwise.
func (s *Sink) scoped(ctx context.Context, explicit string) (string, error) {
	if s.scope == nil {
		return explicit, nil
	}
	tenant, err := s.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", auditlog.ErrScope, err)
	}
	if tenant == "" {
		return "", auditlog.ErrScope
	}
	if explicit != "" && explicit != tenant {
		return "", auditlog.ErrTenantMismatch
	}
	return tenant, nil
}

func scanEvents(rows pgx.Rows, capacity int) ([]auditlog.Event, error) {
	defer rows.Close()
	events := make([]auditlog.Event, 0, capacity)
	for rows.Next() {
		var (
			e       auditlog.Event
			outcome string
		)
		if err := rows.Scan(&e.ID, &e.Tenant, &e.Actor, &e.Action, &e.Resource,
			&outcome, &e.Meta, &e.PrevHash, &e.Hash, &e.Time); err != nil {
			return nil, err
		}
		e.Outcome = auditlog.Outcome(outcome)
		e.Time = e.Time.UTC()
		events = append(events, e)
	}
	return events, rows.Err()
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
