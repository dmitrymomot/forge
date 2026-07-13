package rng_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/gaming/rng"
)

type ctxKey struct{}

func newManager(t *testing.T, opts ...rng.Option) (*rng.Manager, rng.Store) {
	t.Helper()
	store := rng.NewMemoryStore()
	m, err := rng.NewManager(store, opts...)
	require.NoError(t, err)
	return m, store
}

func TestNewManager_Validation(t *testing.T) {
	t.Parallel()
	_, err := rng.NewManager(nil)
	assert.Error(t, err)
	_, err = rng.NewManager(rng.NewMemoryStore(), rng.WithClock(nil))
	assert.Error(t, err)
	_, err = rng.NewManager(rng.NewMemoryStore(), rng.WithScope(nil))
	assert.Error(t, err)
}

func TestActiveSeed_GetOrCreate(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	seed, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)
	assert.NotEmpty(t, seed.ID)
	assert.Len(t, seed.Commitment, 64)
	assert.Nil(t, seed.ServerSeed, "active seed never exposes the server seed")
	assert.NotEmpty(t, seed.ClientSeed)
	assert.Zero(t, seed.Nonce)
	assert.Equal(t, rng.StatusActive, seed.Status)
	assert.Equal(t, rng.Algorithm, seed.Algorithm)

	again, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, seed.ID, again.ID, "second call returns the same pair")

	_, err = m.ActiveSeed(ctx, "")
	assert.Error(t, err, "empty player id")
}

func TestPlay_NoncesAndProof(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	_, p0, err := m.Play(ctx, "p1") // auto-creates the pair
	require.NoError(t, err)
	assert.Zero(t, p0.Nonce)
	assert.NotEmpty(t, p0.SeedID)
	assert.Len(t, p0.Commitment, 64)
	assert.Equal(t, rng.Algorithm, p0.Algorithm)

	_, p1, err := m.Play(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), p1.Nonce)
	assert.Equal(t, p0.SeedID, p1.SeedID)
}

func TestPlay_VerificationRoundTrip(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	// Play 3 rounds, recording outcomes.
	type round struct {
		proof rng.Proof
		stops []int
	}
	rounds := make([]round, 3)
	for i := range rounds {
		stream, proof, err := m.Play(ctx, "p1")
		require.NoError(t, err)
		rounds[i] = round{proof: proof, stops: stream.Ints(5, 100)}
	}

	// Rotate to reveal, then verify every round like a player would.
	old, next, err := m.Rotate(ctx, "p1")
	require.NoError(t, err)
	require.NotNil(t, old.ServerSeed)
	assert.Equal(t, rng.StatusRevealed, old.Status)
	assert.NotEqual(t, old.ID, next.ID)
	assert.Equal(t, old.ClientSeed, next.ClientSeed, "rotation inherits the client seed")

	for i, r := range rounds {
		assert.True(t, rng.VerifyCommitment(old.ServerSeed, r.proof.Commitment), "round %d commitment", i)
		s, err := rng.New(old.ServerSeed, r.proof.ClientSeed, r.proof.Nonce)
		require.NoError(t, err)
		assert.Equal(t, r.stops, s.Ints(5, 100), "round %d replay", i)
	}
}

func TestSetClientSeed(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	// First call with no pair: creates one with the given seed.
	seed, err := m.SetClientSeed(ctx, "p1", "my-lucky-charm")
	require.NoError(t, err)
	assert.Equal(t, "my-lucky-charm", seed.ClientSeed)
	assert.Zero(t, seed.Nonce)

	_, _, err = m.Play(ctx, "p1")
	require.NoError(t, err)

	// Changing it rotates: old revealed, new pair fresh.
	next, err := m.SetClientSeed(ctx, "p1", "second-charm")
	require.NoError(t, err)
	assert.Equal(t, "second-charm", next.ClientSeed)
	assert.NotEqual(t, seed.ID, next.ID)
	assert.Zero(t, next.Nonce)

	old, err := m.Seed(ctx, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, rng.StatusRevealed, old.Status)
	assert.NotNil(t, old.ServerSeed, "old pair revealed for verification")

	_, err = m.SetClientSeed(ctx, "p1", "no spaces allowed")
	assert.ErrorIs(t, err, rng.ErrInvalidClientSeed)
}

func TestRotate_RequiresActivePair(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	_, _, err := m.Rotate(context.Background(), "nobody")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestSeed_Lookup(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()
	created, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)

	got, err := m.Seed(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, got.ServerSeed, "unrevealed lookup hides the server seed")
	assert.Equal(t, created.Commitment, got.Commitment)

	_, err = m.Seed(ctx, "nosuch")
	assert.ErrorIs(t, err, rng.ErrNotFound)
}

func TestPlay_HealsAfterCrashedRotate(t *testing.T) {
	t.Parallel()
	m, store := newManager(t)
	ctx := context.Background()
	seed, err := m.ActiveSeed(ctx, "p1")
	require.NoError(t, err)

	// Simulate a rotate that crashed between reveal and create.
	_, err = store.Reveal(ctx, "", seed.ID, time.Now().UTC())
	require.NoError(t, err)

	_, proof, err := m.Play(ctx, "p1")
	require.NoError(t, err)
	assert.NotEqual(t, seed.ID, proof.SeedID, "fresh pair created")
	assert.Zero(t, proof.Nonce)
}

func TestScope_FailClosedAndIsolation(t *testing.T) {
	t.Parallel()
	hook := func(ctx context.Context) (string, error) {
		v, _ := ctx.Value(ctxKey{}).(string)
		return v, nil
	}
	m, _ := newManager(t, rng.WithScope(hook))

	ctxA := context.WithValue(context.Background(), ctxKey{}, "tenant-a")
	ctxB := context.WithValue(context.Background(), ctxKey{}, "tenant-b")

	seedA, err := m.ActiveSeed(ctxA, "p1")
	require.NoError(t, err)
	seedB, err := m.ActiveSeed(ctxB, "p1")
	require.NoError(t, err)
	assert.NotEqual(t, seedA.ID, seedB.ID, "same player id, different tenants, different pairs")

	_, err = m.Seed(ctxB, seedA.ID)
	assert.ErrorIs(t, err, rng.ErrNotFound, "cross-tenant lookup denied")

	// Empty scope from a configured hook fails closed.
	_, err = m.ActiveSeed(context.Background(), "p1")
	assert.ErrorIs(t, err, rng.ErrNoScope)
	_, _, err = m.Play(context.Background(), "p1")
	assert.ErrorIs(t, err, rng.ErrNoScope)

	// Hook errors fail closed too.
	broken, err := rng.NewManager(rng.NewMemoryStore(), rng.WithScope(func(context.Context) (string, error) {
		return "", errors.New("no tenant")
	}))
	require.NoError(t, err)
	_, err = broken.ActiveSeed(context.Background(), "p1")
	assert.ErrorIs(t, err, rng.ErrNoScope)
}

func TestManager_StoreFailuresWrapErrStore(t *testing.T) {
	t.Parallel()
	m, err := rng.NewManager(failingStore{})
	require.NoError(t, err)
	ctx := context.Background()

	_, err = m.ActiveSeed(ctx, "p1")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, _, err = m.Play(ctx, "p1")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, err = m.SetClientSeed(ctx, "p1", "seed")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, _, err = m.Rotate(ctx, "p1")
	assert.ErrorIs(t, err, rng.ErrStore)
	_, err = m.Seed(ctx, "s1")
	assert.ErrorIs(t, err, rng.ErrStore)
}

func TestManager_ClockStampsCreatedAt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	m, _ := newManager(t, rng.WithClock(clock.NewMock(now)))
	seed, err := m.ActiveSeed(context.Background(), "p1")
	require.NoError(t, err)
	assert.Equal(t, now, seed.CreatedAt)
}

func TestPlay_ConcurrentUniqueNonces(t *testing.T) {
	t.Parallel()
	m, _ := newManager(t)
	ctx := context.Background()

	const n = 50
	proofs := make([]rng.Proof, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, proofs[i], errs[i] = m.Play(ctx, "p1")
		})
	}
	wg.Wait()
	seen := make(map[uint64]bool, n)
	for i, p := range proofs {
		require.NoError(t, errs[i])
		assert.False(t, seen[p.Nonce], "duplicate nonce %d", p.Nonce)
		seen[p.Nonce] = true
	}
}
