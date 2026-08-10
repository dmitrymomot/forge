package apikey_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

// mkKey builds a record with a handcrafted ID so ordering tests are
// deterministic (UUIDv7 ids from the same millisecond have random tails).
func mkKey(b byte, hash, subject, tenant string) apikey.Key {
	return apikey.Key{
		ID:        id.UUID{15: b}, // last byte varies → byte-ascending = ascending b
		Hash:      hash,
		Preview:   "key_preview1",
		Subject:   subject,
		Tenant:    tenant,
		Scopes:    []string{"read"},
		Meta:      map[string]string{"k": "v"},
		CreatedAt: time.Now().UTC(),
	}
}

func TestMemoryStore_CreateGet(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	k := mkKey(1, "h1", "u1", "t1")

	require.NoError(t, s.Create(ctx, k))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, k.Subject, got.Subject)
	assert.Equal(t, k.Hash, got.Hash)

	byHash, err := s.GetByHash(ctx, "h1")
	require.NoError(t, err)
	assert.Equal(t, k.ID, byHash.ID)
}

func TestMemoryStore_NotFound(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()

	_, err := s.Get(ctx, id.UUID{15: 9})
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	_, err = s.GetByHash(ctx, "missing")
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.ErrorIs(t, s.Revoke(ctx, id.UUID{15: 9}, time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Expire(ctx, id.UUID{15: 9}, time.Now()), apikey.ErrNotFound)
	assert.ErrorIs(t, s.Touch(ctx, id.UUID{15: 9}, time.Now()), apikey.ErrNotFound)
}

func TestMemoryStore_Duplicate(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkKey(1, "h1", "u1", "")))

	assert.ErrorIs(t, s.Create(ctx, mkKey(1, "h-other", "u1", "")), apikey.ErrDuplicate) // same ID
	assert.ErrorIs(t, s.Create(ctx, mkKey(2, "h1", "u1", "")), apikey.ErrDuplicate)      // same hash
}

func TestMemoryStore_ListFilterAndOrder(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkKey(1, "h1", "u1", "t1")))
	require.NoError(t, s.Create(ctx, mkKey(2, "h2", "u2", "t1")))
	require.NoError(t, s.Create(ctx, mkKey(3, "h3", "u1", "t2")))

	all, err := s.List(ctx, apikey.Filter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	// Newest first = descending ID bytes.
	assert.Equal(t, id.UUID{15: 3}, all[0].ID)
	assert.Equal(t, id.UUID{15: 1}, all[2].ID)

	t1, err := s.List(ctx, apikey.Filter{Tenant: "t1"})
	require.NoError(t, err)
	assert.Len(t, t1, 2)

	u1t1, err := s.List(ctx, apikey.Filter{Subject: "u1", Tenant: "t1"})
	require.NoError(t, err)
	require.Len(t, u1t1, 1)
	assert.Equal(t, id.UUID{15: 1}, u1t1[0].ID)
}

func TestMemoryStore_Mutators(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	k := mkKey(1, "h1", "u1", "")
	require.NoError(t, s.Create(ctx, k))
	at := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.Revoke(ctx, k.ID, at))
	require.NoError(t, s.Expire(ctx, k.ID, at.Add(time.Hour)))
	require.NoError(t, s.Touch(ctx, k.ID, at.Add(time.Minute)))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.True(t, got.RevokedAt.Equal(at))
	assert.True(t, got.ExpiresAt.Equal(at.Add(time.Hour)))
	assert.True(t, got.LastUsedAt.Equal(at.Add(time.Minute)))
}

func TestMemoryStore_CloneIsolation(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	k := mkKey(1, "h1", "u1", "")
	require.NoError(t, s.Create(ctx, k))

	// Mutating the caller's copy or a returned copy must not affect storage.
	k.Meta["k"] = "mutated"
	got1, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, "v", got1.Meta["k"])

	got1.Meta["k"] = "mutated-again"
	got1.Scopes[0] = "write"
	got2, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, "v", got2.Meta["k"])
	assert.Equal(t, "read", got2.Scopes[0])
}

// TestMemoryStore_NilScopesMetaNormalized pins the read shape every Store
// must return: nil Scopes/Meta are stored and returned as non-nil empties,
// and a List with no matches returns a non-nil empty slice — so callers see
// identical shapes whichever Store backs the Manager.
func TestMemoryStore_NilScopesMetaNormalized(t *testing.T) {
	t.Parallel()
	s := apikey.NewMemoryStore()
	ctx := context.Background()
	k := apikey.Key{
		ID:        id.UUID{15: 1},
		Hash:      "h1",
		Preview:   "key_preview1",
		Subject:   "u1",
		Scopes:    nil, // caller passed nil …
		Meta:      nil, // … for both reference fields
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, s.Create(ctx, k))

	got, err := s.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.Scopes)
	assert.Empty(t, got.Scopes)
	assert.NotNil(t, got.Meta)
	assert.Empty(t, got.Meta)

	byHash, err := s.GetByHash(ctx, "h1")
	require.NoError(t, err)
	assert.NotNil(t, byHash.Scopes)
	assert.NotNil(t, byHash.Meta)

	listed, err := s.List(ctx, apikey.Filter{Subject: "u1"})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].Scopes)
	assert.NotNil(t, listed[0].Meta)

	// No matches → non-nil empty slice, never nil.
	none, err := s.List(ctx, apikey.Filter{Subject: "nobody"})
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}
