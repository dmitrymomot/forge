package approval_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

// actor builds an Actor for subject id.
func actor(id string) approval.Actor {
	return approval.Actor{Subject: access.Subject{ID: id}}
}

// submitted returns a fresh manager and one Pending request.
func submitted(t *testing.T, p approval.Policy, extra ...approval.Option) (*approval.Manager, approval.Request) {
	t.Helper()
	m := newManager(t, p, extra...)
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "invoice #4471"})
	require.NoError(t, err)
	return m, r
}

func TestApproveReachesQuorum(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	ctx := context.Background()

	got, err := m.Approve(ctx, r.ID, approval.Actor{
		Subject: access.Subject{ID: "bob"}, Reason: "matched to invoice"})
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status, "1 of 2")
	require.Len(t, got.Decisions, 1)
	assert.Equal(t, "bob", got.Decisions[0].Approver)
	assert.Equal(t, approval.VoteApprove, got.Decisions[0].Vote)
	assert.Equal(t, "matched to invoice", got.Decisions[0].Reason)
	assert.True(t, got.DecidedAt.IsZero(), "not decided until quorum")

	got, err = m.Approve(ctx, r.ID, actor("carol"))
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status, "2 of 2")
	assert.Len(t, got.Decisions, 2)
	assert.True(t, fixedNow.Equal(got.DecidedAt))
	assert.Equal(t, int64(3), got.Version, "one version bump per vote")
}

func TestRejectIsTerminalAtOneVote(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 3})
	ctx := context.Background()

	got, err := m.Reject(ctx, r.ID, approval.Actor{
		Subject: access.Subject{ID: "bob"}, Reason: "duplicate payment"})
	require.NoError(t, err)
	assert.Equal(t, approval.Rejected, got.Status, "one reject ends it regardless of quorum")
	assert.Equal(t, "duplicate payment", got.Decisions[0].Reason)

	_, err = m.Approve(ctx, r.ID, actor("carol"))
	assert.ErrorIs(t, err, approval.ErrNotPending, "a rejected request never becomes approved")
}

func TestSelfApprovalRejected(t *testing.T) {
	t.Parallel()
	for _, quorum := range []int{1, 2} {
		m, r := submitted(t, approval.Policy{Quorum: quorum})
		_, err := m.Approve(context.Background(), r.ID, actor("alice"))
		assert.ErrorIs(t, err, approval.ErrSelfApproval,
			"quorum %d: the maker is never a checker", quorum)

		_, err = m.Reject(context.Background(), r.ID, actor("alice"))
		assert.ErrorIs(t, err, approval.ErrSelfApproval, "quorum %d: nor may they reject", quorum)
	}
}

func TestDoubleVoteRejected(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 3})
	ctx := context.Background()

	_, err := m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	_, err = m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrAlreadyVoted)

	_, err = m.Reject(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrAlreadyVoted, "cannot switch a vote either")
}

func TestVoteRequiresActorID(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	_, err := m.Approve(context.Background(), r.ID, approval.Actor{})
	assert.ErrorIs(t, err, approval.ErrActorRequired)
}

func TestVoteOnExpiredRequest(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	clk.Set(fixedNow.Add(2 * time.Hour))
	_, err = m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrExpired)
}

func TestVoteOnUnknownRequest(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	_, err := m.Approve(context.Background(), id.NewUUID(), actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotFound)
}

// TestConcurrentApprovalsRespectQuorum is the test this whole package
// exists for: many checkers voting at once must produce exactly one
// Approved transition and exactly one recorded vote per approver.
func TestConcurrentApprovalsRespectQuorum(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2},
		approval.WithMaxRetries(20))
	ctx := context.Background()

	approvers := []string{"bob", "carol", "dave", "erin", "frank"}
	var wg sync.WaitGroup
	errs := make([]error, len(approvers))
	for i, name := range approvers {
		wg.Go(func() {
			_, errs[i] = m.Approve(ctx, r.ID, actor(name))
		})
	}
	wg.Wait()

	got, err := m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)
	assert.Len(t, got.Decisions, 2, "voting stops at quorum; late voters are refused")

	seen := map[string]bool{}
	for _, d := range got.Decisions {
		assert.False(t, seen[d.Approver], "approver %s counted twice", d.Approver)
		seen[d.Approver] = true
	}

	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case assert.ErrorIs(t, err, approval.ErrNotPending):
			refused++
		}
	}
	assert.Equal(t, 2, ok, "exactly quorum many votes succeed")
	assert.Equal(t, len(approvers)-2, refused)
}
