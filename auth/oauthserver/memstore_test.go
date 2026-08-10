package oauthserver_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func TestMemoryStoreCRUD(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	ctx := context.Background()
	c := oauthserver.Client{
		ID: "client_1", Name: "partner", SecretHash: []byte{1, 2},
		Scopes: []string{"read"}, Grants: []string{oauthserver.GrantClientCredentials},
		TenantID: "t1", CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, s.Create(ctx, c))
	require.ErrorIs(t, s.Create(ctx, c), oauthserver.ErrDuplicateClient)

	got, err := s.Get(ctx, "client_1")
	require.NoError(t, err)
	assert.Equal(t, "partner", got.Name)

	_, err = s.Get(ctx, "nope")
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound)

	got.Name = "renamed"
	require.NoError(t, s.Update(ctx, got))
	got2, _ := s.Get(ctx, "client_1")
	assert.Equal(t, "renamed", got2.Name)

	require.ErrorIs(t, s.Update(ctx, oauthserver.Client{ID: "nope"}), oauthserver.ErrClientNotFound)

	require.NoError(t, s.Delete(ctx, "client_1"))
	_, err = s.Get(ctx, "client_1")
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound)
}

func TestMemoryStoreListTenantFilter(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "a", TenantID: "t1"}))
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "b", TenantID: "t2"}))
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "c"}))

	all, err := s.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	t1, err := s.List(ctx, "t1")
	require.NoError(t, err)
	require.Len(t, t1, 1)
	assert.Equal(t, "a", t1[0].ID)
}

// TestMemoryStoreListEmptyIsNonNil pins the Store.List contract (every
// implementation must return []Client{}, not nil, on an empty result set):
// JSON callers must see `[]`, never `null`.
func TestMemoryStoreListEmptyIsNonNil(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	out, err := s.List(context.Background(), "no-such-tenant")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Len(t, out, 0)
}

// TestMemoryStoreListOrderedByCreatedAtThenID pins the Store.List order
// contract (CreatedAt ascending, ties broken by ID): clients are
// created out of chronological order to prove List sorts rather than
// returning map-iteration order.
func TestMemoryStoreListOrderedByCreatedAtThenID(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	ctx := context.Background()
	base := time.Now().UTC()
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "z", CreatedAt: base.Add(2 * time.Second)}))
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "a", CreatedAt: base}))
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "b", CreatedAt: base})) // ties "a" on CreatedAt

	out, err := s.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, out, 3)
	got := []string{out[0].ID, out[1].ID, out[2].ID}
	assert.Equal(t, []string{"a", "b", "z"}, got)
}

func TestMemoryStoreReturnsCopies(t *testing.T) {
	s := oauthserver.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, oauthserver.Client{ID: "a", Scopes: []string{"read"}}))
	got, _ := s.Get(ctx, "a")
	got.Scopes[0] = "mutated"
	fresh, _ := s.Get(ctx, "a")
	assert.Equal(t, "read", fresh.Scopes[0], "stored record must not alias returned slices")
}
