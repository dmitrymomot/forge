//go:build integration

package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
	"github.com/dmitrymomot/forge/web/shortlink"
	"github.com/dmitrymomot/forge/web/shortlink/pgstore"
)

var _ shortlink.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_shortlink_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

// mkLink builds a record whose code and tenant are unique per call: the
// table persists across test runs, so deterministic values would collide on
// the primary key or inflate List counts on re-runs.
func mkLink(t *testing.T) shortlink.Link {
	t.Helper()
	return shortlink.Link{
		Code:      "c" + random.String(15, shortlink.Alphabet),
		URL:       "https://example.com/" + t.Name(),
		Tenant:    "tenant-" + random.Hex(8),
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestPg_CreateGetRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	l := mkLink(t)
	l.ExpiresAt = time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)

	require.NoError(t, s.Create(ctx, l))
	got, err := s.Get(ctx, l.Code)
	require.NoError(t, err)
	assert.Equal(t, l.Code, got.Code)
	assert.Equal(t, l.URL, got.URL)
	assert.Equal(t, l.Tenant, got.Tenant)
	assert.True(t, l.CreatedAt.Equal(got.CreatedAt))
	assert.True(t, l.ExpiresAt.Equal(got.ExpiresAt))
	assert.True(t, got.DeactivatedAt.IsZero())
}

func TestPg_CreateDuplicate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	l := mkLink(t)

	require.NoError(t, s.Create(ctx, l))
	assert.ErrorIs(t, s.Create(ctx, l), shortlink.ErrDuplicate)
}

func TestPg_GetNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.Get(context.Background(), "missing-"+random.Hex(8))
	assert.ErrorIs(t, err, shortlink.ErrNotFound)
}

func TestPg_ZeroTimesRoundTripAsNull(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	l := mkLink(t)

	require.NoError(t, s.Create(ctx, l))
	got, err := s.Get(ctx, l.Code)
	require.NoError(t, err)
	assert.True(t, got.ExpiresAt.IsZero())
	assert.True(t, got.DeactivatedAt.IsZero())
}

func TestPg_ListOrderAndFilter(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tenant := "tenant-" + random.Hex(8)
	base := time.Now().UTC().Truncate(time.Millisecond)

	older := mkLink(t)
	older.Tenant = tenant
	older.CreatedAt = base.Add(-time.Hour)
	newer := mkLink(t)
	newer.Tenant = tenant
	newer.CreatedAt = base
	require.NoError(t, s.Create(ctx, older))
	require.NoError(t, s.Create(ctx, newer))

	got, err := s.List(ctx, shortlink.Filter{Tenant: tenant})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, newer.Code, got[0].Code)
	assert.Equal(t, older.Code, got[1].Code)

	none, err := s.List(ctx, shortlink.Filter{Tenant: "tenant-absent-" + random.Hex(8)})
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestPg_ListTieBreaksByCode(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tenant := "tenant-" + random.Hex(8)
	at := time.Now().UTC().Truncate(time.Millisecond)

	a := mkLink(t)
	a.Code = "a" + random.String(15, shortlink.Alphabet)
	a.Tenant = tenant
	a.CreatedAt = at
	z := mkLink(t)
	z.Code = "z" + random.String(15, shortlink.Alphabet)
	z.Tenant = tenant
	z.CreatedAt = at
	require.NoError(t, s.Create(ctx, z))
	require.NoError(t, s.Create(ctx, a))

	got, err := s.List(ctx, shortlink.Filter{Tenant: tenant})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, a.Code, got[0].Code)
	assert.Equal(t, z.Code, got[1].Code)
}

func TestPg_DeactivateActivate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	l := mkLink(t)
	require.NoError(t, s.Create(ctx, l))

	at := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.Deactivate(ctx, l.Code, at))
	got, err := s.Get(ctx, l.Code)
	require.NoError(t, err)
	assert.True(t, at.Equal(got.DeactivatedAt))

	require.NoError(t, s.Activate(ctx, l.Code))
	got, err = s.Get(ctx, l.Code)
	require.NoError(t, err)
	assert.True(t, got.DeactivatedAt.IsZero())

	missing := "missing-" + random.Hex(8)
	assert.ErrorIs(t, s.Deactivate(ctx, missing, at), shortlink.ErrNotFound)
	assert.ErrorIs(t, s.Activate(ctx, missing), shortlink.ErrNotFound)
}

func TestPg_Delete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	l := mkLink(t)
	require.NoError(t, s.Create(ctx, l))

	require.NoError(t, s.Delete(ctx, l.Code))
	_, err := s.Get(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrNotFound)
	assert.ErrorIs(t, s.Delete(ctx, l.Code), shortlink.ErrNotFound)
}

func TestPg_ManagerEndToEnd(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mgr := shortlink.New(s)

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com/e2e"})
	require.NoError(t, err)

	got, err := mgr.Resolve(ctx, l.Code)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/e2e", got.URL)

	require.NoError(t, mgr.Deactivate(ctx, l.Code))
	_, err = mgr.Resolve(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrLinkDeactivated)

	require.NoError(t, mgr.Delete(ctx, l.Code))
	_, err = mgr.Resolve(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrNotFound)
}
