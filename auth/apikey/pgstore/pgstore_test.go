//go:build integration

package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/apikey/pgstore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ apikey.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_apikey_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

// mkKey builds a record whose hash/subject/tenant are unique per call:
// the table persists across test runs, so deterministic values would
// collide on the unique hash index or inflate List counts on re-runs.
func mkKey(t *testing.T) apikey.Key {
	t.Helper()
	uid := id.NewUUID()
	return apikey.Key{
		ID:        uid,
		Hash:      "hash-" + uid.String(),
		Preview:   "key_preview1",
		Name:      "key-" + t.Name(),
		Subject:   "subj-" + uid.String(),
		Tenant:    "tenant-" + uid.String(),
		Scopes:    []string{"read", "write"},
		Meta:      map[string]string{"env": "prod"},
		CreatedAt: time.Now().UTC(),
	}
}

func TestPg_CreateGetRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k := mkKey(t)
	k.ExpiresAt = time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.Create(ctx, k))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, k.ID, got.ID)
	assert.Equal(t, k.Hash, got.Hash)
	assert.Equal(t, k.Scopes, got.Scopes)
	assert.Equal(t, k.Meta, got.Meta)
	assert.True(t, got.ExpiresAt.Equal(k.ExpiresAt))
	// NULL ⇔ zero-time mapping.
	assert.True(t, got.LastUsedAt.IsZero())
	assert.True(t, got.RevokedAt.IsZero())

	byHash, err := s.GetByHash(ctx, k.Hash)
	require.NoError(t, err)
	assert.Equal(t, k.ID, byHash.ID)
}

func TestPg_NotFound(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, err := s.Get(ctx, id.NewUUID())
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	_, err = s.GetByHash(ctx, "missing-"+id.NewUUID().String())
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.ErrorIs(t, s.Revoke(ctx, id.NewUUID(), time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Expire(ctx, id.NewUUID(), time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Touch(ctx, id.NewUUID(), time.Now()), apikey.ErrNotFound)
}

// TestPg_ListEmptyNonNil pins parity with the memory store: a List with no
// matches returns a non-nil empty slice, never nil.
func TestPg_ListEmptyNonNil(t *testing.T) {
	s := newStore(t)
	none, err := s.List(context.Background(), apikey.Filter{Tenant: "tenant-" + id.NewUUID().String()})
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestPg_DuplicateHash(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k := mkKey(t)
	require.NoError(t, s.Create(ctx, k))

	dup := mkKey(t)
	dup.Hash = k.Hash
	assert.ErrorIs(t, s.Create(ctx, dup), apikey.ErrDuplicate)

	dupID := mkKey(t)
	dupID.ID = k.ID
	assert.ErrorIs(t, s.Create(ctx, dupID), apikey.ErrDuplicate)
}

func TestPg_ListFilterAndOrder(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k1, k2 := mkKey(t), mkKey(t)
	k2.Tenant = k1.Tenant
	// Deterministic id ordering: id.NewUUID() is NOT monotonic within one
	// millisecond, so same-ms ids sort randomly and would flake this test.
	// Share one id and vary only the last byte: k2 > k1 by construction.
	k2.ID = k1.ID
	k1.ID[15], k2.ID[15] = 0x01, 0x02
	require.NoError(t, s.Create(ctx, k1))
	require.NoError(t, s.Create(ctx, k2))

	all, err := s.List(ctx, apikey.Filter{Tenant: k1.Tenant})
	require.NoError(t, err)
	require.Len(t, all, 2)
	// Newest first = descending id bytes.
	assert.Equal(t, k2.ID, all[0].ID)

	one, err := s.List(ctx, apikey.Filter{Tenant: k1.Tenant, Subject: k1.Subject})
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, k1.ID, one[0].ID)
}

func TestPg_Mutators(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	k := mkKey(t)
	require.NoError(t, s.Create(ctx, k))
	at := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.Revoke(ctx, k.ID, at))
	require.NoError(t, s.Expire(ctx, k.ID, at.Add(time.Hour)))
	require.NoError(t, s.Touch(ctx, k.ID, at.Add(time.Minute)))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.True(t, got.RevokedAt.Equal(at))
	assert.True(t, got.ExpiresAt.Equal(at.Add(time.Hour)))
	assert.True(t, got.LastUsedAt.Equal(at.Add(time.Minute)))
}

func TestPg_ManagerEndToEnd(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mgr := apikey.New(s, apikey.WithPrefix("sk_pg"))

	k, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: t.Name() + "-u", Tenant: t.Name() + "-t"})
	require.NoError(t, err)

	identity, err := mgr.Verify(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, t.Name()+"-u", identity.Subject)

	fresh, freshPlain, err := mgr.Rotate(ctx, k.ID, time.Hour)
	require.NoError(t, err)
	_, err = mgr.Verify(ctx, plaintext) // old still inside grace
	require.NoError(t, err)
	_, err = mgr.Verify(ctx, freshPlain)
	require.NoError(t, err)

	require.NoError(t, mgr.Revoke(ctx, fresh.ID))
	_, err = mgr.Verify(ctx, freshPlain)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)
}
