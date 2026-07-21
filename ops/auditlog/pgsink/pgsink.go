package pgsink

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
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
// empty stream, letting a chained Recorder resume across restarts. Like
// every read it honors WithScope: the hook's tenant selects the stream,
// failing closed.
func (s *Sink) ChainHead(ctx context.Context, stream string) (string, error) {
	tenant, err := s.scoped(ctx, stream)
	if err != nil {
		return "", err
	}
	var hash string
	err = s.pool.QueryRow(ctx,
		`SELECT hash FROM forge_audit_events WHERE tenant = $1 ORDER BY id DESC LIMIT 1`,
		tenant).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

// Filter narrows List. Zero-value fields do not filter; a zero Limit
// defaults to 50 (capped at 500). Cursor is the next-page cursor returned
// by the previous List call. Only (tenant, id) is indexed — the
// actor/action/resource/outcome/time filters scan within the tenant's
// history, which is fine for a paged UI but not for hot-path lookups; add
// a purpose-built index in the consumer schema if a filtered query
// becomes hot.
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
	// The WHERE clause carries only the filters actually set, rather than
	// the static `($n = '' OR col = $n)` / `($n IS NULL OR ...)` idiom:
	// once a prepared statement switches to a generic plan (pgx prepares
	// every query, and Postgres goes generic after five executions) those
	// ORs cannot be pruned and the planner stops using
	// forge_audit_events_list_idx. The filter combinations bound the
	// statement cache at 2^8 shapes.
	var (
		conds []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if tenant != "" {
		conds = append(conds, "tenant = "+arg(tenant))
	}
	if f.Actor != "" {
		conds = append(conds, "actor = "+arg(f.Actor))
	}
	if f.Action != "" {
		conds = append(conds, "action = "+arg(f.Action))
	}
	if f.Resource != "" {
		conds = append(conds, "resource = "+arg(f.Resource))
	}
	if f.Outcome != "" {
		conds = append(conds, "outcome = "+arg(string(f.Outcome)))
	}
	if !f.From.IsZero() {
		conds = append(conds, "occurred_at >= "+arg(f.From))
	}
	if !f.To.IsZero() {
		conds = append(conds, "occurred_at <= "+arg(f.To))
	}
	if f.Cursor != "" {
		u, err := id.ParseUUID(f.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %w", ErrInvalidCursor, err)
		}
		conds = append(conds, "id < "+arg(u))
	}
	sql := `SELECT ` + cols + ` FROM forge_audit_events`
	if len(conds) > 0 {
		sql += " WHERE " + strings.Join(conds, " AND ")
	}
	sql += " ORDER BY id DESC LIMIT " + arg(limit+1)

	rows, err := s.pool.Query(ctx, sql, args...)
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
// verified and the final head hash. A tampered, deleted, or reordered
// event fails with auditlog.ErrChainBroken naming the first bad event.
// Under WithScope the hook's tenant selects the stream, failing closed
// like List.
//
// An intact chain proves no one modified the middle of the trail, but an
// attacker who can rewrite rows can also recompute every hash after the
// edit, and truncating the tail leaves a shorter chain that still
// verifies. Compare the returned head (and count) against a copy anchored
// outside the database — exported after each backup, or shipped to
// another system — to detect those.
func (s *Sink) Verify(ctx context.Context, stream string) (int, string, error) {
	tenant, err := s.scoped(ctx, stream)
	if err != nil {
		return 0, "", err
	}
	var (
		prev  string
		after any
		count int
	)
	for {
		rows, err := s.verifyPage(ctx, tenant, after)
		if err != nil {
			return count, prev, err
		}
		events, err := scanEvents(rows, verifyBatch)
		if err != nil {
			return count, prev, err
		}
		if len(events) == 0 {
			return count, prev, nil
		}
		if prev, err = auditlog.VerifyChain(prev, events); err != nil {
			return count, prev, err
		}
		count += len(events)
		after = events[len(events)-1].ID
		if len(events) < verifyBatch {
			return count, prev, nil
		}
	}
}

// verifyPage fetches one ascending batch of the tenant's chain. Two static
// shapes instead of `($2 IS NULL OR id > $2)`: under a generic plan (which
// pgx-prepared statements reach after five executions) the keyset bound
// stops being an index condition, so every batch would rescan the tenant's
// rows from the chain's genesis and filter — O(N²/batch) over the stream.
func (s *Sink) verifyPage(ctx context.Context, tenant string, after any) (pgx.Rows, error) {
	if after == nil {
		return s.pool.Query(ctx, `SELECT `+cols+` FROM forge_audit_events
			WHERE tenant = $1
			ORDER BY id ASC
			LIMIT $2`, tenant, verifyBatch)
	}
	return s.pool.Query(ctx, `SELECT `+cols+` FROM forge_audit_events
		WHERE tenant = $1 AND id > $2
		ORDER BY id ASC
		LIMIT $3`, tenant, after, verifyBatch)
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
