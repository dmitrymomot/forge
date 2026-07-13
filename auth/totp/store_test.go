package totp_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

func rec(confirmed bool) *totp.Record {
	return &totp.Record{
		Secret:       []byte("ciphertext"),
		Confirmed:    confirmed,
		BackupHashes: [][]byte{{1, 2}, {3, 4}},
	}
}

func TestMemoryStore_CRUD(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()

	_, err := s.Get(ctx, "", "alice")
	assert.ErrorIs(t, err, totp.ErrNotFound)

	require.NoError(t, s.Save(ctx, "", "alice", rec(false)))
	got, err := s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.False(t, got.Confirmed)
	assert.Equal(t, []byte("ciphertext"), got.Secret)

	// Save is a full replace.
	require.NoError(t, s.Save(ctx, "", "alice", rec(true)))
	got, err = s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.True(t, got.Confirmed)

	require.NoError(t, s.Delete(ctx, "", "alice"))
	_, err = s.Get(ctx, "", "alice")
	assert.ErrorIs(t, err, totp.ErrNotFound)

	// Delete of an absent record is a no-op, not an error.
	require.NoError(t, s.Delete(ctx, "", "alice"))
}

func TestMemoryStore_DeepCopies(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()
	r := rec(true)
	require.NoError(t, s.Save(ctx, "", "alice", r))

	// Mutating the caller's record after Save must not affect the store.
	r.Secret[0] = 'X'
	r.BackupHashes[0][0] = 99
	got, err := s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.Equal(t, []byte("ciphertext"), got.Secret)
	assert.Equal(t, byte(1), got.BackupHashes[0][0])

	// Mutating a Get result must not affect the store either.
	got.Secret[0] = 'Y'
	again, err := s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.Equal(t, []byte("ciphertext"), again.Secret)
}

func TestMemoryStore_MarkUsed(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()
	require.NoError(t, s.Save(ctx, "", "alice", rec(true)))

	t0 := time.Unix(3000, 0)
	ok, err := s.MarkUsed(ctx, "", "alice", t0)
	require.NoError(t, err)
	assert.True(t, ok, "zero LastUsedAt accepts any step")

	ok, err = s.MarkUsed(ctx, "", "alice", t0)
	require.NoError(t, err)
	assert.False(t, ok, "same step is claimed exactly once")

	ok, err = s.MarkUsed(ctx, "", "alice", time.Unix(2970, 0))
	require.NoError(t, err)
	assert.False(t, ok, "earlier step rejected")

	ok, err = s.MarkUsed(ctx, "", "alice", time.Unix(3030, 0))
	require.NoError(t, err)
	assert.True(t, ok, "later step advances")

	ok, err = s.MarkUsed(ctx, "", "ghost", t0)
	require.NoError(t, err)
	assert.False(t, ok, "absent record: false, nil (pg driver parity)")
}

func TestMemoryStore_MarkUsed_ConcurrentSingleWinner(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()
	require.NoError(t, s.Save(ctx, "", "alice", rec(true)))

	const n = 32
	wins := make(chan bool, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			ok, err := s.MarkUsed(ctx, "", "alice", time.Unix(3000, 0))
			assert.NoError(t, err)
			wins <- ok
		})
	}
	wg.Wait()
	close(wins)
	won := 0
	for ok := range wins {
		if ok {
			won++
		}
	}
	assert.Equal(t, 1, won, "exactly one concurrent verify claims the step")
}

func TestMemoryStore_ConsumeBackup(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()
	require.NoError(t, s.Save(ctx, "", "alice", rec(true)))

	ok, err := s.ConsumeBackup(ctx, "", "alice", []byte{1, 2})
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = s.ConsumeBackup(ctx, "", "alice", []byte{1, 2})
	require.NoError(t, err)
	assert.False(t, ok, "single use")

	got, err := s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.Equal(t, [][]byte{{3, 4}}, got.BackupHashes)

	ok, err = s.ConsumeBackup(ctx, "", "ghost", []byte{3, 4})
	require.NoError(t, err)
	assert.False(t, ok, "absent record: false, nil")
}

func TestMemoryStore_TenantIsolationAndDeleteTenant(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()
	require.NoError(t, s.Save(ctx, "t1", "alice", rec(true)))
	require.NoError(t, s.Save(ctx, "t2", "alice", rec(false)))
	require.NoError(t, s.Save(ctx, "", "alice", rec(true)))

	r1, err := s.Get(ctx, "t1", "alice")
	require.NoError(t, err)
	assert.True(t, r1.Confirmed)
	r2, err := s.Get(ctx, "t2", "alice")
	require.NoError(t, err)
	assert.False(t, r2.Confirmed, "same subject, distinct records per tenant")

	n, err := s.DeleteTenant(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	_, err = s.Get(ctx, "t1", "alice")
	assert.ErrorIs(t, err, totp.ErrNotFound)
	_, err = s.Get(ctx, "t2", "alice")
	assert.NoError(t, err, "other tenants untouched")
	_, err = s.Get(ctx, "", "alice")
	assert.NoError(t, err, "unscoped record untouched")

	n, err = s.DeleteTenant(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
