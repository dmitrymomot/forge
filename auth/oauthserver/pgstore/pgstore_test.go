//go:build integration

package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/auth/oauthserver/pgstore"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ oauthserver.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_oauthserver_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func sample(id, tenant string) oauthserver.Client {
	return oauthserver.Client{
		ID: id, Name: "n-" + id, SecretHash: []byte{1, 2, 3},
		Scopes: []string{"read"}, Grants: []string{oauthserver.GrantClientCredentials},
		RedirectURIs: []string{"https://m.example.com/cb"},
		TenantID:     tenant, TokenTTL: 5 * time.Minute,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestPgStoreCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	id := "client_" + t.Name()
	_ = s.Delete(ctx, id)

	c := sample(id, "t1")
	require.NoError(t, s.Create(ctx, c))
	require.ErrorIs(t, s.Create(ctx, c), oauthserver.ErrDuplicateClient)

	got, err := s.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, c.Name, got.Name)
	assert.Equal(t, c.SecretHash, got.SecretHash)
	assert.Equal(t, c.Scopes, got.Scopes)
	assert.Equal(t, c.Grants, got.Grants)
	assert.Equal(t, c.RedirectURIs, got.RedirectURIs)
	assert.Equal(t, 5*time.Minute, got.TokenTTL)
	assert.False(t, got.Revoked())
	assert.WithinDuration(t, c.CreatedAt, got.CreatedAt, time.Second)

	got.RevokedAt = time.Now().UTC()
	got.Name = "renamed"
	require.NoError(t, s.Update(ctx, got))
	got2, err := s.Get(ctx, id)
	require.NoError(t, err)
	assert.True(t, got2.Revoked())
	assert.Equal(t, "renamed", got2.Name)

	require.NoError(t, s.Delete(ctx, id))
	_, err = s.Get(ctx, id)
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound)
	require.ErrorIs(t, s.Update(ctx, got), oauthserver.ErrClientNotFound)
}

func TestPgStoreListTenantFilter(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, b := "client_"+t.Name()+"_a", "client_"+t.Name()+"_b"
	_ = s.Delete(ctx, a)
	_ = s.Delete(ctx, b)
	require.NoError(t, s.Create(ctx, sample(a, "tenant-list-1")))
	require.NoError(t, s.Create(ctx, sample(b, "tenant-list-2")))

	t1, err := s.List(ctx, "tenant-list-1")
	require.NoError(t, err)
	require.Len(t, t1, 1)
	assert.Equal(t, a, t1[0].ID)

	all, err := s.List(ctx, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)
}
