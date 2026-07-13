package totp_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

func TestMemoryStore_SavePending(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()

	// Absent → stores a fresh pending record.
	ok, err := s.SavePending(ctx, "", "alice", &totp.Record{Secret: []byte("s1")})
	require.NoError(t, err)
	assert.True(t, ok)
	got, err := s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.False(t, got.Confirmed)
	assert.Equal(t, []byte("s1"), got.Secret)
	assert.Empty(t, got.BackupHashes)

	// Pending → overwrites with a fresh secret, still pending.
	ok, err = s.SavePending(ctx, "", "alice", &totp.Record{Secret: []byte("s2")})
	require.NoError(t, err)
	assert.True(t, ok)
	got, err = s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.Equal(t, []byte("s2"), got.Secret)

	// Confirm it, then SavePending must refuse to clobber the confirmed record.
	ok, err = s.Confirm(ctx, "", "alice", []byte("s2"), time.Unix(3000, 0), [][]byte{{9}})
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = s.SavePending(ctx, "", "alice", &totp.Record{Secret: []byte("s3")})
	require.NoError(t, err)
	assert.False(t, ok, "must not overwrite a confirmed enrollment")
	got, err = s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.True(t, got.Confirmed)
	assert.Equal(t, []byte("s2"), got.Secret, "confirmed record untouched")
}

func TestMemoryStore_Confirm(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()

	// Absent → false.
	ok, err := s.Confirm(ctx, "", "alice", []byte("s1"), time.Unix(3000, 0), [][]byte{{1}})
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = s.SavePending(ctx, "", "alice", &totp.Record{Secret: []byte("s1")})
	require.NoError(t, err)
	require.True(t, ok)

	// Wrong secret → false, stays pending (a racing SavePending swapped it).
	ok, err = s.Confirm(ctx, "", "alice", []byte("WRONG"), time.Unix(3000, 0), [][]byte{{1}})
	require.NoError(t, err)
	assert.False(t, ok)
	got, err := s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.False(t, got.Confirmed)

	// Right secret → activates with hashes + last-used step.
	at := time.Unix(3000, 0)
	ok, err = s.Confirm(ctx, "", "alice", []byte("s1"), at, [][]byte{{1, 2}, {3, 4}})
	require.NoError(t, err)
	assert.True(t, ok)
	got, err = s.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.True(t, got.Confirmed)
	assert.True(t, got.LastUsedAt.Equal(at))
	assert.Equal(t, [][]byte{{1, 2}, {3, 4}}, got.BackupHashes)

	// Already confirmed → false.
	ok, err = s.Confirm(ctx, "", "alice", []byte("s1"), at, [][]byte{{9}})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestMemoryStore_Confirm_ConcurrentSingleWinner(t *testing.T) {
	t.Parallel()
	s := totp.NewMemoryStore()
	ctx := t.Context()
	ok, err := s.SavePending(ctx, "", "alice", &totp.Record{Secret: []byte("s1")})
	require.NoError(t, err)
	require.True(t, ok)

	const n = 32
	wins := make(chan bool, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			won, err := s.Confirm(ctx, "", "alice", []byte("s1"), time.Unix(3000, 0), [][]byte{{1}})
			assert.NoError(t, err)
			wins <- won
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
	assert.Equal(t, 1, won, "exactly one concurrent confirm claims the pending enrollment")
}
