//go:build integration

package pgstore_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/gaming/rng"
	"github.com/dmitrymomot/forge/gaming/rng/pgstore"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ rng.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_rng_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func testSeedBytes() []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	return seed
}

// mkRecord builds a record with unique id and player per call: the table
// persists across test runs, so fixed values would collide.
func mkRecord(scope string) rng.Record {
	return rng.Record{
		ID:         id.NewUUID().String(),
		Scope:      scope,
		PlayerID:   "p-" + id.NewUUID().String(),
		ServerSeed: testSeedBytes(),
		ClientSeed: "alice",
		Status:     rng.StatusActive,
		Algorithm:  rng.Algorithm,
		CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestPg_CreateActiveRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	got, err := s.Active(ctx, "", rec.PlayerID)
	require.NoError(t, err)
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, rec.ServerSeed, got.ServerSeed)
	assert.Equal(t, rec.ClientSeed, got.ClientSeed)
	assert.Equal(t, rng.Algorithm, got.Algorithm)
	assert.Zero(t, got.Nonce)
	assert.True(t, got.RevealedAt.IsZero())

	_, err = s.Active(ctx, "", "p-nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Active(ctx, "other", rec.PlayerID)
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPg_CreateConflicts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	second := mkRecord("")
	second.PlayerID = rec.PlayerID
	assert.ErrorIs(t, s.Create(ctx, second), rng.ErrExists, "second active for same player")

	dup := mkRecord("")
	dup.ID = rec.ID
	assert.ErrorIs(t, s.Create(ctx, dup), rng.ErrExists, "duplicate id")

	scoped := mkRecord("acme")
	scoped.PlayerID = rec.PlayerID
	assert.NoError(t, s.Create(ctx, scoped), "same player, different scope")
}

func TestPg_ConsumeNonce(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	for want := range uint64(3) {
		got, err := s.ConsumeNonce(ctx, "", rec.PlayerID)
		require.NoError(t, err)
		assert.Equal(t, want, got.Nonce, "consumed value is pre-increment")
		assert.Equal(t, rec.ID, got.ID)
		assert.Equal(t, rec.ServerSeed, got.ServerSeed)
	}
	act, err := s.Active(ctx, "", rec.PlayerID)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), act.Nonce)

	_, err = s.ConsumeNonce(ctx, "", "p-nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPg_Reveal(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))
	at := time.Now().UTC().Truncate(time.Microsecond)

	got, err := s.Reveal(ctx, "", rec.ID, at)
	require.NoError(t, err)
	assert.Equal(t, rng.StatusRevealed, got.Status)
	assert.Equal(t, at, got.RevealedAt.UTC())

	_, err = s.Active(ctx, "", rec.PlayerID)
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.ConsumeNonce(ctx, "", rec.PlayerID)
	assert.ErrorIs(t, err, rng.ErrNotFound)

	again, err := s.Reveal(ctx, "", rec.ID, at.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, at, again.RevealedAt.UTC(), "reveal is idempotent")

	_, err = s.Reveal(ctx, "", id.NewUUID().String(), at)
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Reveal(ctx, "wrong-scope", rec.ID, at)
	assert.ErrorIs(t, err, rng.ErrNotFound)

	// A fresh active pair can be created after reveal.
	next := mkRecord("")
	next.PlayerID = rec.PlayerID
	assert.NoError(t, s.Create(ctx, next))
}

func TestPg_Get(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("acme")
	require.NoError(t, s.Create(ctx, rec))
	got, err := s.Get(ctx, "acme", rec.ID)
	require.NoError(t, err)
	assert.Equal(t, rec.PlayerID, got.PlayerID)
	_, err = s.Get(ctx, "", rec.ID)
	assert.ErrorIs(t, err, rng.ErrNotFound, "scope mismatch")
	_, err = s.Get(ctx, "acme", id.NewUUID().String())
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPg_ConcurrentConsumeUniqueNonces(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord("")
	require.NoError(t, s.Create(ctx, rec))

	const n = 20
	nonces := make([]uint64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			var got rng.Record
			got, errs[i] = s.ConsumeNonce(ctx, "", rec.PlayerID)
			nonces[i] = got.Nonce
		})
	}
	wg.Wait()
	seen := make(map[uint64]bool, n)
	for i, v := range nonces {
		require.NoError(t, errs[i])
		assert.False(t, seen[v], "duplicate nonce %d", v)
		seen[v] = true
	}
}

// TestPg_ManagerEndToEnd runs the full provably-fair lifecycle against
// real Postgres: play, rotate, verify.
func TestPg_ManagerEndToEnd(t *testing.T) {
	s := newStore(t)
	m, err := rng.NewManager(s)
	require.NoError(t, err)
	ctx := context.Background()
	player := "p-" + id.NewUUID().String()

	stream, proof, err := m.Play(ctx, player)
	require.NoError(t, err)
	stops := stream.Ints(5, 100)

	old, next, err := m.Rotate(ctx, player)
	require.NoError(t, err)
	require.NotNil(t, old.ServerSeed)
	assert.NotEqual(t, old.ID, next.ID)

	assert.True(t, rng.VerifyCommitment(old.ServerSeed, proof.Commitment))
	replay, err := rng.New(old.ServerSeed, proof.ClientSeed, proof.Nonce)
	require.NoError(t, err)
	assert.Equal(t, stops, replay.Ints(5, 100))
}
