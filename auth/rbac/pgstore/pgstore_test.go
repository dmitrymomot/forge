//go:build integration

package pgstore_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/rbac"
	"github.com/dmitrymomot/forge/auth/rbac/pgstore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ rbac.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_rbac_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func TestPgAssignRolesForUnassign(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	subject := id.NewUUID().String() // unique per run; table persists
	tenant := "acme"

	require.NoError(t, s.Assign(ctx, tenant, subject, []string{"editor", "auditor"}))
	require.NoError(t, s.Assign(ctx, tenant, subject, []string{"editor"})) // idempotent

	roles, err := s.RolesFor(ctx, tenant, subject)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"editor", "auditor"}, roles)

	// tenant isolation
	other, err := s.RolesFor(ctx, "other", subject)
	require.NoError(t, err)
	assert.Empty(t, other)

	require.NoError(t, s.Unassign(ctx, tenant, subject, []string{"auditor"}))
	roles, err = s.RolesFor(ctx, tenant, subject)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"editor"}, roles)
}
