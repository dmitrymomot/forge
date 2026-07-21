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
	m, r := submitted(t, approval.Policy{Quorum: 1},
		approval.WithAuditor(auditlog.New(sink)))
	ctx := context.Background()

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

	assert.Equal(t, "approval.claim", events[2].Action)
	assert.Equal(t, "worker-1", events[2].Actor)

	assert.Equal(t, "approval.complete", events[3].Action)
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

func TestNoAuditorIsSilent(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1})
	_, err := m.Approve(context.Background(), r.ID, actor("bob"))
	assert.NoError(t, err, "the auditor seam is optional")
}
