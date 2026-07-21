package approval_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

// approved returns a manager and a request that has reached quorum.
func approved(t *testing.T, p approval.Policy, extra ...approval.Option) (*approval.Manager, approval.Request) {
	t.Helper()
	m, r := submitted(t, p, extra...)
	got, err := m.Approve(context.Background(), r.ID, actor("bob"))
	require.NoError(t, err)
	require.Equal(t, approval.Approved, got.Status)
	return m, got
}

func TestClaimAndComplete(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	got, err := m.Claim(ctx, r.ID, "worker-3")
	require.NoError(t, err)
	assert.Equal(t, approval.Executing, got.Status)
	assert.Equal(t, "worker-3", got.ClaimedBy)
	assert.True(t, fixedNow.Equal(got.ClaimedAt))

	got, err = m.Complete(ctx, r.ID, "worker-3")
	require.NoError(t, err)
	assert.Equal(t, approval.Executed, got.Status)
	assert.True(t, got.Status.Terminal())
}

func TestClaimIsExclusive(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	_, err = m.Claim(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed)
}

func TestClaimRequiresApproved(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	_, err := m.Claim(context.Background(), r.ID, "worker-1")
	assert.ErrorIs(t, err, approval.ErrNotApproved, "a pending request is not executable")
}

func TestClaimRequiresExecutorID(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	_, err := m.Claim(context.Background(), r.ID, "")
	assert.ErrorIs(t, err, approval.ErrExecutorRequired)
}

func TestZeroClaimTTLWedgesUntilRelease(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m, r := approved(t, approval.Policy{Quorum: 1}, approval.WithClock(clk))
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	clk.Set(fixedNow.Add(365 * 24 * time.Hour))
	_, err = m.Claim(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed,
		"ClaimTTL 0 never lets a second executor in — the safe default for money")

	// Release is the escape hatch, and it is deliberately not holder-checked.
	got, err := m.Release(ctx, r.ID, actor("ops-oncall"))
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)
	assert.Empty(t, got.ClaimedBy)
	assert.True(t, got.ClaimedAt.IsZero())

	got, err = m.Claim(ctx, r.ID, "worker-2")
	require.NoError(t, err)
	assert.Equal(t, "worker-2", got.ClaimedBy)
}

func TestStaleClaimIsReclaimable(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m, r := approved(t, approval.Policy{Quorum: 1, ClaimTTL: 5 * time.Minute},
		approval.WithClock(clk))
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	clk.Set(fixedNow.Add(time.Minute))
	_, err = m.Claim(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed, "lease still held")

	clk.Set(fixedNow.Add(5 * time.Minute))
	got, err := m.Claim(ctx, r.ID, "worker-2")
	require.NoError(t, err, "lease expired at exactly ClaimTTL")
	assert.Equal(t, "worker-2", got.ClaimedBy)
}

func TestCompleteAndFailAreHolderChecked(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()
	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	_, err = m.Complete(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrNotClaimHolder)

	_, err = m.Fail(ctx, r.ID, "worker-2", "nope")
	assert.ErrorIs(t, err, approval.ErrNotClaimHolder)
}

func TestCompleteRequiresExecuting(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	_, err := m.Complete(context.Background(), r.ID, "worker-1")
	assert.ErrorIs(t, err, approval.ErrNotExecuting)
}

func TestFailRecordsReason(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()
	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	got, err := m.Fail(ctx, r.ID, "worker-1", "gateway rejected: insufficient funds")
	require.NoError(t, err)
	assert.Equal(t, approval.Failed, got.Status)
	assert.Equal(t, "gateway rejected: insufficient funds", got.Meta["approval.failure"])
}

func TestApprovedRequestExpiresBeforeClaim(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 1, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	_, err = m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	clk.Set(fixedNow.Add(2 * time.Hour))
	_, err = m.Claim(ctx, r.ID, "worker-1")
	assert.ErrorIs(t, err, approval.ErrExpired, "an approved-but-unexecuted request is not immortal")
}

func TestExecutingDoesNotExpire(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 1, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	_, err = m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)
	_, err = m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	clk.Set(fixedNow.Add(2 * time.Hour))
	got, err := m.Complete(ctx, r.ID, "worker-1")
	require.NoError(t, err, "TTL does not reap an in-flight action; ClaimTTL governs it")
	assert.Equal(t, approval.Executed, got.Status)
}

// TestConcurrentClaimsHaveOneWinner is the execute-once guarantee.
func TestConcurrentClaimsHaveOneWinner(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1}, approval.WithMaxRetries(20))
	ctx := context.Background()

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Go(func() {
			_, errs[i] = m.Claim(ctx, r.ID, "worker")
		})
	}
	wg.Wait()

	var won int
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			assert.ErrorIs(t, err, approval.ErrAlreadyClaimed)
		}
	}
	assert.Equal(t, 1, won, "exactly one executor claims the action")
}

func TestExecuteWrapsTheTrio(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	var sawPayload payoutPayload
	got, err := m.Execute(ctx, r.ID, "worker-1", func(ctx context.Context, req approval.Request) error {
		p, err := approval.PayloadOf(kindPayout, req)
		require.NoError(t, err)
		sawPayload = p
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, approval.Executed, got.Status)
	assert.Equal(t, "po_88", sawPayload.PayoutID)
}

func TestExecuteFailsOnActionError(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	boom := errors.New("gateway down")

	got, err := m.Execute(context.Background(), r.ID, "worker-1",
		func(context.Context, approval.Request) error { return boom })
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, approval.Failed, got.Status, "the failure is recorded, not swallowed")
}

// TestExecuteRunsFnWhenClaimAuditFails guards the fix: Claim returns
// (r, m.audit(...)), so a durable claim whose trail write failed used to
// look like a failed claim to Execute, which returned Request{} without
// ever calling fn — wedging the request in Executing with no way for the
// caller to recover it. fn must still run and Complete must still apply,
// with ErrAuditFailed surfacing on the final error rather than vanishing.
func TestExecuteRunsFnWhenClaimAuditFails(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 1},
		approval.WithAuditor(auditlog.New(failingSink{})))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice"})
	require.ErrorIs(t, err, approval.ErrAuditFailed)

	_, err = m.Approve(ctx, r.ID, actor("bob"))
	require.ErrorIs(t, err, approval.ErrAuditFailed)

	var ran bool
	var sawPayload payoutPayload
	got, err := m.Execute(ctx, r.ID, "worker-1", func(_ context.Context, req approval.Request) error {
		ran = true
		p, perr := approval.PayloadOf(kindPayout, req)
		sawPayload = p
		return perr
	})
	assert.ErrorIs(t, err, approval.ErrAuditFailed,
		"a durable claim whose own audit write failed must stay visible, not vanish")
	assert.True(t, ran, "fn must run even though the claim's own audit write failed")
	assert.Equal(t, approval.Executed, got.Status,
		"the claim was durable, so Complete must still have applied")
	assert.Equal(t, "po_88", sawPayload.PayoutID)
}

// TestExecuteJoinsDistinctAuditFailures covers the ambiguity fix: when
// BOTH the claim's audit write and the subsequent Complete audit write
// fail, the joined error must carry two ErrAuditFailed wrappings that are
// textually distinguishable — a caller reading the error string alone
// must be able to tell the claim's trail entry and the completion's
// trail entry apart, not see the same message twice with no way to know
// which transition lost its record.
func TestExecuteJoinsDistinctAuditFailures(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 1},
		approval.WithAuditor(auditlog.New(failingSink{})))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout,
		payoutPayload{PayoutID: "po_88"}, approval.SubmitParams{Requester: "alice"})
	require.ErrorIs(t, err, approval.ErrAuditFailed)

	_, err = m.Approve(ctx, r.ID, actor("bob"))
	require.ErrorIs(t, err, approval.ErrAuditFailed)

	got, err := m.Execute(ctx, r.ID, "worker-1",
		func(context.Context, approval.Request) error { return nil })
	require.ErrorIs(t, err, approval.ErrAuditFailed)
	assert.Equal(t, approval.Executed, got.Status, "both durable transitions still applied")

	msg := err.Error()
	assert.Contains(t, msg, "approval.claim",
		"the claim's own audit failure must name its transition")
	assert.Contains(t, msg, "approval.complete",
		"the completion's audit failure must name its transition, distinctly from the claim's")
}

func TestExecuteYieldsToTheClaimHolder(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()
	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	var ran bool
	_, err = m.Execute(ctx, r.ID, "worker-2", func(context.Context, approval.Request) error {
		ran = true
		return nil
	})
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed)
	assert.False(t, ran, "the action must not run without the claim")
}
