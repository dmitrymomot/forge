package totp_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
	"github.com/dmitrymomot/forge/core/clock"
)

// enroll provisions a confirmed enrollment and returns the manager,
// enrollment (for code computation), and issued backup codes.
func enroll(t *testing.T, store totp.Store, opts ...totp.Option) (*totp.Manager, *totp.Enrollment, []string) {
	t.Helper()
	m := newManager(t, store, opts...)
	ctx := t.Context()
	enr, err := m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)
	backup, err := m.ConfirmEnroll(ctx, "alice", codeFor(t, enr, fixedNow))
	require.NoError(t, err)
	return m, enr, backup
}

func TestManagerVerify_TOTPPath(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	_, enr, _ := enroll(t, store)
	ctx := t.Context()

	// ConfirmEnroll consumed the fixedNow step; advance one step.
	later := fixedNow.Add(30 * time.Second)
	m2, err := totp.NewManager(store, testKey, totp.WithClock(clock.NewMock(later)))
	require.NoError(t, err)

	res, err := m2.Verify(ctx, "alice", codeFor(t, enr, later))
	require.NoError(t, err)
	assert.False(t, res.UsedBackupCode)

	// Same code again: the step is spent.
	_, err = m2.Verify(ctx, "alice", codeFor(t, enr, later))
	assert.ErrorIs(t, err, totp.ErrReplayed)

	// Garbage code.
	_, err = m2.Verify(ctx, "alice", "000000")
	assert.ErrorIs(t, err, totp.ErrInvalidCode)
}

func TestManagerVerify_BackupPath(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m, _, backup := enroll(t, store)
	ctx := t.Context()

	res, err := m.Verify(ctx, "alice", backup[0])
	require.NoError(t, err)
	assert.True(t, res.UsedBackupCode)
	assert.Equal(t, 9, res.BackupRemaining)

	// Consumed: second use fails.
	_, err = m.Verify(ctx, "alice", backup[0])
	assert.ErrorIs(t, err, totp.ErrInvalidCode)

	// Normalization: uppercase + no dashes still verifies.
	res, err = m.Verify(ctx, "alice", "  "+stringsUpperNoDash(backup[1])+" ")
	require.NoError(t, err)
	assert.True(t, res.UsedBackupCode)
	assert.Equal(t, 8, res.BackupRemaining)
}

func stringsUpperNoDash(s string) string {
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		c := s[i]
		if c == '-' {
			continue
		}
		if 'a' <= c && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

func TestManagerVerify_Unenrolled(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store)
	ctx := t.Context()

	_, err := m.Verify(ctx, "ghost", "123456")
	assert.ErrorIs(t, err, totp.ErrNotEnrolled)

	// Pending (unconfirmed) enrollment refuses Verify too.
	_, err = m.BeginEnroll(ctx, "bob", "bob@acme.com")
	require.NoError(t, err)
	_, err = m.Verify(ctx, "bob", "123456")
	assert.ErrorIs(t, err, totp.ErrNotEnrolled)
}

func TestManagerVerify_ConcurrentSameCode_OneWinner(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	_, enr, _ := enroll(t, store)
	ctx := t.Context()

	later := fixedNow.Add(60 * time.Second)
	m2, err := totp.NewManager(store, testKey, totp.WithClock(clock.NewMock(later)))
	require.NoError(t, err)
	code := codeFor(t, enr, later)

	const n = 16
	results := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := m2.Verify(ctx, "alice", code)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	okCount, replayCount := 0, 0
	for err := range results {
		switch {
		case err == nil:
			okCount++
		case assert.ErrorIs(t, err, totp.ErrReplayed):
			replayCount++
		}
	}
	assert.Equal(t, 1, okCount, "exactly one concurrent verify succeeds")
	assert.Equal(t, n-1, replayCount)
}

func TestEnabledAndLastVerified(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store)
	ctx := t.Context()

	on, err := m.Enabled(ctx, "alice")
	require.NoError(t, err)
	assert.False(t, on, "absent: false, nil — a policy query, not an error")
	_, err = m.LastVerified(ctx, "alice")
	assert.ErrorIs(t, err, totp.ErrNotEnrolled)

	enr, err := m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)
	on, err = m.Enabled(ctx, "alice")
	require.NoError(t, err)
	assert.False(t, on, "pending: still false")
	_, err = m.LastVerified(ctx, "alice")
	assert.ErrorIs(t, err, totp.ErrNotEnrolled, "pending: not enrolled")

	_, err = m.ConfirmEnroll(ctx, "alice", codeFor(t, enr, fixedNow))
	require.NoError(t, err)
	on, err = m.Enabled(ctx, "alice")
	require.NoError(t, err)
	assert.True(t, on)
	last, err := m.LastVerified(ctx, "alice")
	require.NoError(t, err)
	assert.False(t, last.IsZero(), "confirm stamped the first verified step")
}
