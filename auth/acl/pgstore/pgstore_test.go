//go:build integration

package pgstore_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/acl"
	"github.com/dmitrymomot/forge/auth/acl/pgstore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ acl.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_acl_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func TestPgPutEntriesForDelete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	subject := id.NewUUID().String() // unique per run; table persists
	tenant := "acme"

	entries := []acl.Entry{
		{Subject: subject, ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow},
		{Subject: subject, ResourceType: "agent", ResourceID: "13", Action: "agents:read", Effect: access.Deny},
		{Subject: subject, ResourceType: "agent", ResourceID: "", Action: "agents:list", Effect: access.Allow},
		{Subject: subject, ResourceType: "report", ResourceID: "42", Action: "reports:read", Effect: access.Allow},
	}
	require.NoError(t, s.Put(ctx, tenant, entries))
	require.NoError(t, s.Put(ctx, tenant, entries[:1])) // idempotent

	// exact id + type-wide, same type only
	got, err := s.EntriesFor(ctx, tenant, subject, "agent", "42")
	require.NoError(t, err)
	assert.ElementsMatch(t, []acl.Entry{entries[0], entries[2]}, got)

	// collection check: only type-wide entries
	got, err = s.EntriesFor(ctx, tenant, subject, "agent", "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []acl.Entry{entries[2]}, got)

	// tenant isolation
	got, err = s.EntriesFor(ctx, "other", subject, "agent", "42")
	require.NoError(t, err)
	assert.Empty(t, got)

	all, err := s.ListFor(ctx, tenant, subject)
	require.NoError(t, err)
	assert.ElementsMatch(t, entries, all)

	require.NoError(t, s.Delete(ctx, tenant, subject, "agent", "42", []string{"agents:read"}))
	require.NoError(t, s.Delete(ctx, tenant, subject, "agent", "42", []string{"agents:read"})) // missing: not an error
	all, err = s.ListFor(ctx, tenant, subject)
	require.NoError(t, err)
	assert.ElementsMatch(t, []acl.Entry{entries[1], entries[2], entries[3]}, all)
}

func TestPgPutFlipsEffect(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	subject := id.NewUUID().String()

	grant := acl.Entry{Subject: subject, ResourceType: "agent", ResourceID: "42", Action: "agents:read", Effect: access.Allow}
	deny := grant
	deny.Effect = access.Deny

	require.NoError(t, s.Put(ctx, "", []acl.Entry{grant}))
	require.NoError(t, s.Put(ctx, "", []acl.Entry{deny}))

	got, err := s.ListFor(ctx, "", subject)
	require.NoError(t, err)
	require.Len(t, got, 1) // one key — the effect flipped in place
	assert.Equal(t, access.Deny, got[0].Effect)
}

func TestPgPutRejectsInvalidEffect(t *testing.T) {
	s := newStore(t)
	err := s.Put(context.Background(), "", []acl.Entry{
		{Subject: id.NewUUID().String(), ResourceType: "agent", ResourceID: "42", Action: "agents:read"}, // zero Effect
	})
	assert.ErrorIs(t, err, acl.ErrInvalidEntry)
}
