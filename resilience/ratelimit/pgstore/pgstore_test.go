package pgstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/dmitrymomot/forge/resilience/ratelimit/pgstore"
)

var _ ratelimit.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_ratelimit_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func TestPgCounter_IncrTTLAndNoExpiry(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.Reset(ctx, "c1"))
	require.NoError(t, s.Reset(ctx, "g1"))

	n, err := s.Incr(ctx, "c1", 3, 500*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	n, err = s.Incr(ctx, "c1", 2, 500*time.Millisecond) // must not extend TTL
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)
	time.Sleep(700 * time.Millisecond)
	got, err := s.Get(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got) // expired

	_, err = s.Incr(ctx, "g1", 4, 0) // no expiry
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	got, err = s.Get(ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, int64(4), got)
	require.NoError(t, s.Reset(ctx, "g1"))
}
