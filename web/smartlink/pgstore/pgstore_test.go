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
	"github.com/dmitrymomot/forge/web/smartlink"
	"github.com/dmitrymomot/forge/web/smartlink/pgstore"
)

var _ smartlink.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_smartlink_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

// code returns a code unique per call: the table persists across test runs,
// so a fixed literal would collide on the code primary key.
func code() string { return "code-" + random.String(12) }

func TestPg_CreateGetRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	c := code()
	created := time.Now().UTC().Truncate(time.Millisecond)
	l := smartlink.Link{
		Code:      c,
		Target:    "https://example.com/",
		Ref:       "ref1",
		Tenant:    "tenant-" + random.String(8),
		Metadata:  map[string]string{"k": "v"},
		CreatedAt: created,
	}
	require.NoError(t, s.Create(ctx, l))

	got, err := s.Get(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, l.Code, got.Code)
	assert.Equal(t, l.Target, got.Target)
	assert.Equal(t, l.Ref, got.Ref)
	assert.Equal(t, l.Tenant, got.Tenant)
	assert.Equal(t, l.Metadata, got.Metadata)
	assert.True(t, got.CreatedAt.Equal(created))
	// zero ExpiresAt/DeactivatedAt round-trip through NULL back to zero.
	assert.True(t, got.ExpiresAt.IsZero())
	assert.True(t, got.DeactivatedAt.IsZero())
}

func TestPg_CreateDuplicate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	c := code()
	require.NoError(t, s.Create(ctx, smartlink.Link{Code: c, CreatedAt: time.Now().UTC()}))
	err := s.Create(ctx, smartlink.Link{Code: c, CreatedAt: time.Now().UTC()})
	assert.ErrorIs(t, err, smartlink.ErrDuplicate)
}

func TestPg_GetNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.Get(context.Background(), code())
	assert.ErrorIs(t, err, smartlink.ErrNotFound)
}

func TestPg_ListOrderFilterLimit(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tenant := "tenant-" + random.String(8)
	other := "tenant-" + random.String(8)
	base := time.Now().UTC().Truncate(time.Millisecond)

	cb, ca, cc, cd := code(), code(), code(), code()
	// cb and ca share CreatedAt: tie is broken by code ascending.
	links := []smartlink.Link{
		{Code: cb, Tenant: tenant, CreatedAt: base},
		{Code: ca, Tenant: tenant, CreatedAt: base},
		{Code: cc, Tenant: other, CreatedAt: base.Add(time.Hour)},
		{Code: cd, Tenant: tenant, CreatedAt: base.Add(2 * time.Hour)},
	}
	for _, l := range links {
		require.NoError(t, s.Create(ctx, l))
	}
	wantOrder := []string{cd, cc, ca, cb}
	if ca > cb {
		wantOrder = []string{cd, cc, cb, ca}
	}

	all, err := s.List(ctx, smartlink.Filter{})
	require.NoError(t, err)
	all = filterCodes(all, cb, ca, cc, cd)
	require.Len(t, all, 4)
	assert.Equal(t, wantOrder, codesOf(all))

	scoped, err := s.List(ctx, smartlink.Filter{Tenant: tenant})
	require.NoError(t, err)
	require.Len(t, scoped, 3)
	for _, l := range scoped {
		assert.Equal(t, tenant, l.Tenant)
	}

	limited, err := s.List(ctx, smartlink.Filter{Tenant: tenant, Limit: 1})
	require.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, cd, limited[0].Code)
}

func TestPg_ListEmptyNonNil(t *testing.T) {
	s := newStore(t)
	none, err := s.List(context.Background(), smartlink.Filter{Tenant: "tenant-" + random.String(12)})
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestPg_TenantPredicateMutators(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Millisecond)

	setup := func(t *testing.T) (*pgstore.Store, string, string) {
		t.Helper()
		s := newStore(t)
		c, owner := code(), "owner-"+random.String(8)
		require.NoError(t, s.Create(ctx, smartlink.Link{Code: c, Tenant: owner, CreatedAt: time.Now().UTC()}))
		return s, c, owner
	}

	t.Run("deactivate wrong tenant", func(t *testing.T) {
		s, c, _ := setup(t)
		err := s.Deactivate(ctx, c, "intruder", at)
		assert.ErrorIs(t, err, smartlink.ErrNotFound)
		got, _ := s.Get(ctx, c)
		assert.True(t, got.DeactivatedAt.IsZero())
	})

	t.Run("deactivate correct tenant", func(t *testing.T) {
		s, c, owner := setup(t)
		require.NoError(t, s.Deactivate(ctx, c, owner, at))
		got, err := s.Get(ctx, c)
		require.NoError(t, err)
		assert.True(t, got.DeactivatedAt.Equal(at))
	})

	t.Run("deactivate zero at leaves active", func(t *testing.T) {
		s, c, owner := setup(t)
		require.NoError(t, s.Deactivate(ctx, c, owner, time.Time{}))
		got, err := s.Get(ctx, c)
		require.NoError(t, err)
		assert.True(t, got.DeactivatedAt.IsZero())
	})

	t.Run("deactivate zero at does not reactivate", func(t *testing.T) {
		s, c, owner := setup(t)
		require.NoError(t, s.Deactivate(ctx, c, owner, at))
		require.NoError(t, s.Deactivate(ctx, c, owner, time.Time{}))
		got, err := s.Get(ctx, c)
		require.NoError(t, err)
		assert.True(t, got.DeactivatedAt.Equal(at))
	})

	t.Run("deactivate zero at still enforces predicate", func(t *testing.T) {
		s, c, _ := setup(t)
		err := s.Deactivate(ctx, c, "intruder", time.Time{})
		assert.ErrorIs(t, err, smartlink.ErrNotFound)
	})

	t.Run("activate wrong tenant", func(t *testing.T) {
		s, c, owner := setup(t)
		require.NoError(t, s.Deactivate(ctx, c, owner, at))
		err := s.Activate(ctx, c, "intruder")
		assert.ErrorIs(t, err, smartlink.ErrNotFound)
		got, _ := s.Get(ctx, c)
		assert.False(t, got.DeactivatedAt.IsZero())
	})

	t.Run("activate empty tenant unconstrained", func(t *testing.T) {
		s, c, owner := setup(t)
		require.NoError(t, s.Deactivate(ctx, c, owner, at))
		require.NoError(t, s.Activate(ctx, c, ""))
		got, err := s.Get(ctx, c)
		require.NoError(t, err)
		assert.True(t, got.DeactivatedAt.IsZero())
	})

	t.Run("delete wrong tenant", func(t *testing.T) {
		s, c, _ := setup(t)
		err := s.Delete(ctx, c, "intruder")
		assert.ErrorIs(t, err, smartlink.ErrNotFound)
		_, err = s.Get(ctx, c)
		assert.NoError(t, err)
	})

	t.Run("delete correct tenant", func(t *testing.T) {
		s, c, owner := setup(t)
		require.NoError(t, s.Delete(ctx, c, owner))
		_, err := s.Get(ctx, c)
		assert.ErrorIs(t, err, smartlink.ErrNotFound)
	})
}

func TestPg_DeleteRecreate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	c := code()
	require.NoError(t, s.Create(ctx, smartlink.Link{Code: c, Target: "https://a.example.com/", CreatedAt: time.Now().UTC()}))
	require.NoError(t, s.Delete(ctx, c, ""))
	require.NoError(t, s.Create(ctx, smartlink.Link{Code: c, Target: "https://b.example.com/", CreatedAt: time.Now().UTC()}))

	got, err := s.Get(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, "https://b.example.com/", got.Target)
}

// TestPg_MetadataNilVsEmpty pins the MemoryStore reference contract: nil
// Metadata round-trips as nil (stored as jsonb 'null'), an explicit empty
// map as a non-nil empty map — so `l.Metadata == nil` behaves identically
// whichever Store backs the Manager.
func TestPg_MetadataNilVsEmpty(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	cNil := code()
	require.NoError(t, s.Create(ctx, smartlink.Link{Code: cNil, CreatedAt: time.Now().UTC(), Metadata: nil}))
	gotNil, err := s.Get(ctx, cNil)
	require.NoError(t, err)
	assert.Nil(t, gotNil.Metadata)

	cEmpty := code()
	require.NoError(t, s.Create(ctx, smartlink.Link{Code: cEmpty, CreatedAt: time.Now().UTC(), Metadata: map[string]string{}}))
	gotEmpty, err := s.Get(ctx, cEmpty)
	require.NoError(t, err)
	assert.NotNil(t, gotEmpty.Metadata)
	assert.Empty(t, gotEmpty.Metadata)
}

func codesOf(links []smartlink.Link) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l.Code
	}
	return out
}

// filterCodes narrows links to only those whose code is in want, preserving
// order; the table persists across test runs, so unrelated rows from other
// tests may otherwise pollute an unscoped List.
func filterCodes(links []smartlink.Link, want ...string) []smartlink.Link {
	set := make(map[string]bool, len(want))
	for _, c := range want {
		set[c] = true
	}
	out := make([]smartlink.Link, 0, len(want))
	for _, l := range links {
		if set[l.Code] {
			out = append(out, l)
		}
	}
	return out
}
