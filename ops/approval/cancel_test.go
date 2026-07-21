package approval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/ops/approval"
)

func TestCancelFromPending(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})

	got, err := m.Cancel(context.Background(), r.ID,
		approval.Actor{Subject: access.Subject{ID: "alice"}, Reason: "wrong vendor"})
	require.NoError(t, err)
	assert.Equal(t, approval.Cancelled, got.Status)
	assert.True(t, fixedNow.Equal(got.DecidedAt))
}

func TestCancelFromApproved(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	_, err := m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	got, err := m.Cancel(ctx, r.ID, actor("alice"))
	require.NoError(t, err)
	assert.Equal(t, approval.Cancelled, got.Status, "approved but unexecuted is still cancellable")
}

func TestCancelRejectedOnTerminalRequest(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	ctx := context.Background()

	_, err := m.Reject(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	_, err = m.Cancel(ctx, r.ID, actor("alice"))
	assert.ErrorIs(t, err, approval.ErrNotCancellable)
}

func TestCancelRequiresActorID(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	_, err := m.Cancel(context.Background(), r.ID, approval.Actor{})
	assert.ErrorIs(t, err, approval.ErrActorRequired)
}

func TestCancelRejectedWhileExecuting(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	_, err = m.Cancel(ctx, r.ID, actor("alice"))
	assert.ErrorIs(t, err, approval.ErrNotCancellable, "an in-flight action is not cancellable")
}

func TestCancelRejectedOnExecutedRequest(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)
	_, err = m.Complete(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	_, err = m.Cancel(ctx, r.ID, actor("alice"))
	assert.ErrorIs(t, err, approval.ErrNotCancellable)
}
