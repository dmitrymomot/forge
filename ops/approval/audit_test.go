package approval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

// failingSink rejects every write.
type failingSink struct{}

func (failingSink) Write(context.Context, auditlog.Event) error {
	return errors.New("sink unavailable")
}

func TestAuditRecordsEveryTransition(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	auditor := auditlog.New(sink)
	ctx := context.Background()

	m, r := submitted(t, approval.Policy{Quorum: 1}, approval.WithAuditor(auditor))
	_, err := m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)
	_, err = m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)
	_, err = m.Complete(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	events := sink.Events()
	require.Len(t, events, 4)

	assert.Equal(t, "approval.submit", events[0].Action)
	assert.Equal(t, "alice", events[0].Actor)
	assert.Equal(t, auditlog.OutcomeSuccess, events[0].Outcome)
	assert.Equal(t, "approval:"+r.ID.String(), events[0].Resource)
	assert.Equal(t, "payout.release", events[0].Meta["kind"])

	assert.Equal(t, "approval.approve", events[1].Action)
	assert.Equal(t, "bob", events[1].Actor)
	assert.Equal(t, "approved", events[1].Meta["status"])
	assert.Equal(t, "approve", events[1].Meta["vote"])

	assert.Equal(t, "approval.claim", events[2].Action)
	assert.Equal(t, "worker-1", events[2].Actor)

	assert.Equal(t, "approval.complete", events[3].Action)

	// A second flow, on its own request, covers Fail.
	m2, r2 := submitted(t, approval.Policy{Quorum: 1}, approval.WithAuditor(auditor))
	_, err = m2.Approve(ctx, r2.ID, actor("bob"))
	require.NoError(t, err)
	_, err = m2.Claim(ctx, r2.ID, "worker-2")
	require.NoError(t, err)
	_, err = m2.Fail(ctx, r2.ID, "worker-2", "gateway down")
	require.NoError(t, err)

	events = sink.Events()
	require.Len(t, events, 8)
	assert.Equal(t, "approval.fail", events[7].Action)
	assert.Equal(t, "worker-2", events[7].Actor)
	assert.Equal(t, auditlog.OutcomeFailure, events[7].Outcome)
	assert.Equal(t, "gateway down", events[7].Meta["reason"])

	// A third flow, on its own request, covers Release.
	m3, r3 := submitted(t, approval.Policy{Quorum: 1}, approval.WithAuditor(auditor))
	_, err = m3.Approve(ctx, r3.ID, actor("bob"))
	require.NoError(t, err)
	_, err = m3.Claim(ctx, r3.ID, "worker-3")
	require.NoError(t, err)
	_, err = m3.Release(ctx, r3.ID, actor("ops-oncall"))
	require.NoError(t, err)

	events = sink.Events()
	require.Len(t, events, 12)
	assert.Equal(t, "approval.release", events[11].Action)
	assert.Equal(t, "ops-oncall", events[11].Actor)
	assert.Equal(t, auditlog.OutcomeSuccess, events[11].Outcome)
}

func TestAuditRecordsDeniedAttempts(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 1},
		approval.WithDecider(d), approval.WithAuditor(auditlog.New(sink)))

	_, err := m.Approve(context.Background(), r.ID, actor("mallory"))
	require.ErrorIs(t, err, approval.ErrNotEligible)

	events := sink.Events()
	require.Len(t, events, 2, "submit + the denied attempt")
	denied := events[1]
	assert.Equal(t, "approval.approve", denied.Action)
	assert.Equal(t, auditlog.OutcomeDenied, denied.Outcome)
	assert.Equal(t, "mallory", denied.Actor)
	assert.Equal(t, "approval:"+r.ID.String(), denied.Resource)
}

// TestAuditFailureReturnsDurableRequest deviates from the task brief's
// literal test body: the brief built the fixture via submitted(), which
// asserts require.NoError on Submit — but Submit itself audits, so a sink
// that fails every write also fails submission, and the helper's own
// assertion trips before Approve is ever reached. Submitting directly here
// exercises the identical durable-request-survives-a-failed-trail-write
// contract, just proven at both transitions (Submit and Approve) instead of
// only the second.
func TestAuditFailureReturnsDurableRequest(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 1},
		approval.WithAuditor(auditlog.New(failingSink{})))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "invoice #4471"})
	require.ErrorIs(t, err, approval.ErrAuditFailed)
	require.False(t, r.ID.IsZero(), "the request is durable even though the trail write failed")

	got, err := m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrAuditFailed)
	assert.Equal(t, approval.Approved, got.Status,
		"the transition is durable even though the trail write failed")

	stored, gerr := m.Get(ctx, r.ID)
	require.NoError(t, gerr)
	assert.Equal(t, approval.Approved, stored.Status, "and it really is persisted")
}

// TestDeniedVoteAndFailingSinkJoinsBothErrors covers the vote path (backing
// Approve/Reject): when the decider denies AND the audit sink is down, the
// caller must still see the original ErrNotEligible alongside ErrAuditFailed
// — not have it masked entirely.
func TestDeniedVoteAndFailingSinkJoinsBothErrors(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m := newManager(t, approval.Policy{Quorum: 1},
		approval.WithDecider(d), approval.WithAuditor(auditlog.New(failingSink{})))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "invoice #4471"})
	require.ErrorIs(t, err, approval.ErrAuditFailed)
	require.False(t, r.ID.IsZero())

	_, err = m.Approve(ctx, r.ID, actor("mallory"))
	assert.ErrorIs(t, err, approval.ErrNotEligible,
		"the business-rule denial must survive the audit sink also failing")
	assert.ErrorIs(t, err, approval.ErrAuditFailed)
}

// TestCancelDeniedAndFailingSinkJoinsBothErrors covers the same defect on
// the Cancel path.
func TestCancelDeniedAndFailingSinkJoinsBothErrors(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m := newManager(t, approval.Policy{Quorum: 2},
		approval.WithDecider(d), approval.WithAuditor(auditlog.New(failingSink{})))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "invoice #4471"})
	require.ErrorIs(t, err, approval.ErrAuditFailed)
	require.False(t, r.ID.IsZero())

	_, err = m.Cancel(ctx, r.ID, actor("ops-oncall"))
	assert.ErrorIs(t, err, approval.ErrNotEligible,
		"the business-rule denial must survive the audit sink also failing")
	assert.ErrorIs(t, err, approval.ErrAuditFailed)
}

func TestNoAuditorIsSilent(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1})
	_, err := m.Approve(context.Background(), r.ID, actor("bob"))
	assert.NoError(t, err, "the auditor seam is optional")
}
