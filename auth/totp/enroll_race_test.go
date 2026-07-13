package totp_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

// TestConfirmEnroll_ConcurrentDoubleSubmit proves the atomic Confirm gate: a
// double-submitted first code resolves to exactly one winner — only that call
// stores and returns backup codes, and those are the codes that actually work.
func TestConfirmEnroll_ConcurrentDoubleSubmit(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store)
	ctx := t.Context()

	enr, err := m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)
	code := codeFor(t, enr, fixedNow)

	const n = 16
	type outcome struct {
		codes []string
		err   error
	}
	results := make(chan outcome, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			codes, err := m.ConfirmEnroll(ctx, "alice", code)
			results <- outcome{codes, err}
		})
	}
	wg.Wait()
	close(results)

	var winners [][]string
	already := 0
	for r := range results {
		switch {
		case r.err == nil:
			winners = append(winners, r.codes)
		case errors.Is(r.err, totp.ErrAlreadyEnrolled):
			already++
		default:
			t.Fatalf("unexpected error from concurrent ConfirmEnroll: %v", r.err)
		}
	}
	require.Len(t, winners, 1, "exactly one confirm wins")
	assert.Equal(t, n-1, already, "every loser sees ErrAlreadyEnrolled")
	assert.Len(t, winners[0], 10)

	// The winner's codes are the ones actually persisted.
	res, err := m.Verify(ctx, "alice", winners[0][0])
	require.NoError(t, err)
	assert.True(t, res.UsedBackupCode)
}

// TestBeginEnroll_DoesNotClobberConfirmed proves the atomic SavePending gate:
// once an enrollment is confirmed, BeginEnroll refuses rather than reverting it
// to pending.
func TestBeginEnroll_DoesNotClobberConfirmed(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store)
	ctx := t.Context()

	enr, err := m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)
	_, err = m.ConfirmEnroll(ctx, "alice", codeFor(t, enr, fixedNow))
	require.NoError(t, err)

	_, err = m.BeginEnroll(ctx, "alice", "alice@acme.com")
	assert.ErrorIs(t, err, totp.ErrAlreadyEnrolled)

	// The confirmed enrollment is intact — still enabled.
	on, err := m.Enabled(ctx, "alice")
	require.NoError(t, err)
	assert.True(t, on)
}
