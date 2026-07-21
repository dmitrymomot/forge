//go:build integration

package pgstore_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/approval/approvaltest"
	"github.com/dmitrymomot/forge/ops/approval/pgstore"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ approval.Store = (*pgstore.Store)(nil)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations,
		migration.WithTable("forge_approval_schema")).Up(context.Background(), db))
	return pool
}

// TestPgStoreContract runs the same suite the memory store passes. The
// table outlives the process, so the suite namespaces its own fixtures —
// nothing here truncates.
func TestPgStoreContract(t *testing.T) {
	pool := newPool(t)
	approvaltest.Run(t, func(t *testing.T) approval.Store {
		return pgstore.New(pool)
	})
}

// TestConcurrentUpdatesConflict proves the CAS is enforced by Postgres
// itself, not by a mutex the memory store happens to hold.
func TestConcurrentUpdatesConflict(t *testing.T) {
	s := pgstore.New(newPool(t))
	ctx := context.Background()

	r := approval.Request{
		ID:        id.NewUUID(),
		Kind:      "kind-" + id.NewUUID().String(),
		Tenant:    "tenant-" + id.NewUUID().String(),
		Requester: "alice",
		Status:    approval.Pending,
		Version:   1,
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	require.NoError(t, s.Create(ctx, r))

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Update(ctx, r, 1)
		}()
	}
	wg.Wait()

	var won int
	for _, err := range errs {
		if err == nil {
			won++
			continue
		}
		require.ErrorIs(t, err, approval.ErrConflict)
	}
	require.Equal(t, 1, won, "exactly one CAS wins at a given version")
}
