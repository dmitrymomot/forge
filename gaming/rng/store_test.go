package rng_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func mkRecord(id, scope, player string) rng.Record {
	return rng.Record{
		ID:         id,
		Scope:      scope,
		PlayerID:   player,
		ServerSeed: testSeed(),
		ClientSeed: "alice",
		Status:     rng.StatusActive,
		Algorithm:  rng.Algorithm,
		CreatedAt:  time.Now().UTC(),
	}
}

func TestMemoryStore_CreateActiveRoundTrip(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	rec := mkRecord("s1", "", "p1")
	require.NoError(t, s.Create(ctx, rec))

	got, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, rec.ServerSeed, got.ServerSeed)
	assert.Equal(t, rng.Algorithm, got.Algorithm)
	assert.Zero(t, got.Nonce)

	_, err = s.Active(ctx, "", "nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Active(ctx, "other-scope", "p1")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestMemoryStore_CreateConflicts(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))
	assert.ErrorIs(t, s.Create(ctx, mkRecord("s2", "", "p1")), rng.ErrExists, "second active for same player")
	assert.ErrorIs(t, s.Create(ctx, mkRecord("s1", "", "p9")), rng.ErrExists, "duplicate id")
	assert.NoError(t, s.Create(ctx, mkRecord("s3", "", "p2")), "other player ok")
	assert.NoError(t, s.Create(ctx, mkRecord("s4", "acme", "p1")), "other scope ok")
}

func TestMemoryStore_ConsumeNonce(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))

	for want := range uint64(3) {
		rec, err := s.ConsumeNonce(ctx, "", "p1")
		require.NoError(t, err)
		assert.Equal(t, want, rec.Nonce, "consumed value is pre-increment")
		assert.Equal(t, "s1", rec.ID)
		assert.Equal(t, testSeed(), rec.ServerSeed)
	}
	act, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	assert.Equal(t, uint64(3), act.Nonce, "stored nonce is next-unused")

	_, err = s.ConsumeNonce(ctx, "", "nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestMemoryStore_Reveal(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))
	at := time.Now().UTC().Truncate(time.Second)

	rec, err := s.Reveal(ctx, "", "s1", at)
	require.NoError(t, err)
	assert.Equal(t, rng.StatusRevealed, rec.Status)
	assert.Equal(t, at, rec.RevealedAt)

	_, err = s.Active(ctx, "", "p1")
	assert.ErrorIs(t, err, rng.ErrNotFound, "revealed pair is no longer active")
	_, err = s.ConsumeNonce(ctx, "", "p1")
	assert.ErrorIs(t, err, rng.ErrNotFound, "cannot play a revealed seed")

	again, err := s.Reveal(ctx, "", "s1", at.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, at, again.RevealedAt, "reveal is idempotent")

	_, err = s.Reveal(ctx, "", "nosuch", at)
	assert.ErrorIs(t, err, rng.ErrNotFound)
	_, err = s.Reveal(ctx, "wrong-scope", "s1", at)
	assert.ErrorIs(t, err, rng.ErrNotFound)

	// A fresh active pair can be created after reveal.
	assert.NoError(t, s.Create(ctx, mkRecord("s2", "", "p1")))
}

func TestMemoryStore_Get(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "acme", "p1")))
	got, err := s.Get(ctx, "acme", "s1")
	require.NoError(t, err)
	assert.Equal(t, "p1", got.PlayerID)
	_, err = s.Get(ctx, "", "s1")
	assert.ErrorIs(t, err, rng.ErrNotFound, "scope mismatch")
	_, err = s.Get(ctx, "acme", "nosuch")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestMemoryStore_ConcurrentConsumeUniqueNonces(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))

	const n = 100
	nonces := make([]uint64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			var rec rng.Record
			rec, errs[i] = s.ConsumeNonce(ctx, "", "p1")
			nonces[i] = rec.Nonce
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

func TestMemoryStore_ReturnedRecordIsIsolated(t *testing.T) {
	t.Parallel()
	s := rng.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkRecord("s1", "", "p1")))
	got, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	got.ServerSeed[0] ^= 0xff // mutating the returned copy...
	again, err := s.Active(ctx, "", "p1")
	require.NoError(t, err)
	assert.Equal(t, testSeed(), again.ServerSeed, "...must not corrupt the store")
}
