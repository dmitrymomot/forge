//go:build integration

package pgsink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/ops/auditlog"
	"github.com/dmitrymomot/forge/ops/auditlog/pgsink"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgsink.Migrations, migration.WithTable("forge_auditlog_schema")).Up(context.Background(), db))
	return pool
}

// tenantFor returns a unique tenant per test: the table persists across
// test runs, so a fixed tenant would inflate List counts on re-runs.
func tenantFor(t *testing.T) string {
	t.Helper()
	return "tenant-" + id.NewUUID().String()
}

func record(t *testing.T, rec *auditlog.Recorder, tenant string, n int) []auditlog.Event {
	t.Helper()
	out := make([]auditlog.Event, 0, n)
	for range n {
		e, err := rec.Record(context.Background(), auditlog.Event{
			Tenant: tenant, Actor: "user_1", Action: "doc.edit", Resource: "doc:1",
			Outcome: auditlog.OutcomeSuccess, Meta: map[string]string{"k": "v"},
		})
		require.NoError(t, err)
		out = append(out, e)
	}
	return out
}

func TestPg_WriteListRoundTrip(t *testing.T) {
	sink := pgsink.New(newPool(t))
	tenant := tenantFor(t)
	rec := auditlog.New(sink, auditlog.WithChain())

	at := time.Date(2026, 7, 21, 10, 30, 0, 123456789, time.UTC)
	want, err := rec.Record(context.Background(), auditlog.Event{
		Tenant: tenant, Actor: "user_9", Action: "invoice.void", Resource: "invoice:77",
		Outcome: auditlog.OutcomeDenied, Meta: map[string]string{"reason": "already paid"}, Time: at,
	})
	require.NoError(t, err)

	events, next, err := sink.List(context.Background(), pgsink.Filter{Tenant: tenant})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Empty(t, next)
	assert.Equal(t, want, events[0], "event round-trips bit-identically (microsecond time)")
}

func TestPg_ListFiltersAndPaginates(t *testing.T) {
	sink := pgsink.New(newPool(t))
	tenant := tenantFor(t)
	rec := auditlog.New(sink)
	record(t, rec, tenant, 5)
	otherTenant := tenantFor(t)
	record(t, rec, otherTenant, 3)

	// Tenant isolation.
	events, _, err := sink.List(context.Background(), pgsink.Filter{Tenant: tenant})
	require.NoError(t, err)
	require.Len(t, events, 5)
	for _, e := range events {
		assert.Equal(t, tenant, e.Tenant)
	}

	// Newest first.
	for i := 1; i < len(events); i++ {
		assert.True(t, events[i-1].Time.After(events[i].Time) || events[i-1].Time.Equal(events[i].Time))
	}

	// Keyset pagination walks the full set without overlap.
	var (
		seen   []id.UUID
		cursor string
	)
	for {
		page, next, err := sink.List(context.Background(), pgsink.Filter{Tenant: tenant, Limit: 2, Cursor: cursor})
		require.NoError(t, err)
		for _, e := range page {
			seen = append(seen, e.ID)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	require.Len(t, seen, 5)
	for i, e := range events {
		assert.Equal(t, e.ID, seen[i], "paged walk equals unpaged order")
	}

	// Field filters.
	byActor, _, err := sink.List(context.Background(), pgsink.Filter{Tenant: tenant, Actor: "nobody"})
	require.NoError(t, err)
	assert.Empty(t, byActor)
	byAction, _, err := sink.List(context.Background(), pgsink.Filter{Tenant: tenant, Action: "doc.edit", Outcome: auditlog.OutcomeSuccess})
	require.NoError(t, err)
	assert.Len(t, byAction, 5)

	// Time range.
	mid := events[2].Time
	ranged, _, err := sink.List(context.Background(), pgsink.Filter{Tenant: tenant, From: mid})
	require.NoError(t, err)
	assert.Len(t, ranged, 3, "From is inclusive")
	ranged, _, err = sink.List(context.Background(), pgsink.Filter{Tenant: tenant, To: mid})
	require.NoError(t, err)
	assert.Len(t, ranged, 3, "To is inclusive")
}

func TestPg_ListExactPageBoundary(t *testing.T) {
	sink := pgsink.New(newPool(t))
	tenant := tenantFor(t)
	record(t, auditlog.New(sink), tenant, 4)

	page, next, err := sink.List(context.Background(), pgsink.Filter{Tenant: tenant, Limit: 4})
	require.NoError(t, err)
	require.Len(t, page, 4)
	assert.Empty(t, next, "exact fit is the last page")
}

func TestPg_InvalidCursor(t *testing.T) {
	sink := pgsink.New(newPool(t))
	_, _, err := sink.List(context.Background(), pgsink.Filter{Cursor: "not-a-uuid"})
	assert.ErrorIs(t, err, pgsink.ErrInvalidCursor)
}

func TestPg_ScopedReads(t *testing.T) {
	pool := newPool(t)
	tenant := tenantFor(t)
	record(t, auditlog.New(pgsink.New(pool)), tenant, 2)
	otherTenant := tenantFor(t)
	record(t, auditlog.New(pgsink.New(pool)), otherTenant, 1)

	current := tenant
	scoped := pgsink.New(pool, pgsink.WithScope(func(context.Context) (string, error) {
		return current, nil
	}))

	events, _, err := scoped.List(context.Background(), pgsink.Filter{})
	require.NoError(t, err)
	assert.Len(t, events, 2, "scope confines the query")

	_, _, err = scoped.List(context.Background(), pgsink.Filter{Tenant: otherTenant})
	assert.ErrorIs(t, err, auditlog.ErrTenantMismatch)

	current = ""
	_, _, err = scoped.List(context.Background(), pgsink.Filter{})
	assert.ErrorIs(t, err, auditlog.ErrScope, "empty tenant fails closed")

	hookErr := errors.New("no session")
	failing := pgsink.New(pool, pgsink.WithScope(func(context.Context) (string, error) {
		return "", hookErr
	}))
	_, _, err = failing.List(context.Background(), pgsink.Filter{})
	assert.ErrorIs(t, err, auditlog.ErrScope)
	assert.ErrorIs(t, err, hookErr)
	_, err = failing.Verify(context.Background(), "")
	assert.ErrorIs(t, err, auditlog.ErrScope, "Verify fails closed too")
}

func TestPg_ChainHeadAndResume(t *testing.T) {
	sink := pgsink.New(newPool(t))
	tenant := tenantFor(t)

	head, err := sink.ChainHead(context.Background(), tenant)
	require.NoError(t, err)
	assert.Empty(t, head, "empty stream has no head")

	rec := auditlog.New(sink, auditlog.WithChain())
	events := record(t, rec, tenant, 2)

	head, err = sink.ChainHead(context.Background(), tenant)
	require.NoError(t, err)
	assert.Equal(t, events[1].Hash, head)

	// A fresh recorder (process restart) resumes the persisted chain.
	rec2 := auditlog.New(sink, auditlog.WithChain())
	record(t, rec2, tenant, 2)

	n, err := sink.Verify(context.Background(), tenant)
	require.NoError(t, err)
	assert.Equal(t, 4, n, "chain verifies across recorder restarts")
}

func TestPg_VerifyDetectsTampering(t *testing.T) {
	pool := newPool(t)
	sink := pgsink.New(pool)
	tenant := tenantFor(t)
	events := record(t, auditlog.New(sink, auditlog.WithChain()), tenant, 3)

	n, err := sink.Verify(context.Background(), tenant)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// A DBA rewrites one event's payload behind the application's back.
	_, err = pool.Exec(context.Background(),
		`UPDATE forge_audit_events SET actor = 'attacker' WHERE id = $1`, events[1].ID)
	require.NoError(t, err)

	_, err = sink.Verify(context.Background(), tenant)
	assert.ErrorIs(t, err, auditlog.ErrChainBroken)
	assert.ErrorContains(t, err, events[1].ID.String(), "names the first bad event")

	// Deleting an event breaks the chain at its successor (restore the
	// actor first so the deletion is the only damage).
	_, err = pool.Exec(context.Background(),
		`UPDATE forge_audit_events SET actor = 'user_1' WHERE id = $1`, events[1].ID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`DELETE FROM forge_audit_events WHERE id = $1`, events[0].ID)
	require.NoError(t, err)
	_, err = sink.Verify(context.Background(), tenant)
	assert.ErrorIs(t, err, auditlog.ErrChainBroken)
}

func TestPg_VerifyCrossesBatchBoundary(t *testing.T) {
	sink := pgsink.New(newPool(t))
	tenant := tenantFor(t)
	// More events than one verify batch (500) forces head threading
	// across batches.
	record(t, auditlog.New(sink, auditlog.WithChain()), tenant, 501)

	n, err := sink.Verify(context.Background(), tenant)
	require.NoError(t, err)
	assert.Equal(t, 501, n)
}

func TestPg_VerifyEmptyStream(t *testing.T) {
	sink := pgsink.New(newPool(t))
	n, err := sink.Verify(context.Background(), tenantFor(t))
	require.NoError(t, err)
	assert.Zero(t, n)
}
