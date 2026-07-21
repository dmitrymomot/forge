package approval_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/ops/approval"
)

// recordingDecider captures what the manager asked, and answers with effect.
type recordingDecider struct {
	gotAction   access.Action
	gotResource access.Resource
	gotSubject  access.Subject
	effect      access.Effect
	err         error
}

func (d *recordingDecider) Decide(_ context.Context, s access.Subject, a access.Action, r access.Resource) (access.Decision, error) {
	d.gotSubject, d.gotAction, d.gotResource = s, a, r
	if d.err != nil {
		return access.Decision{}, d.err
	}
	return access.Decision{Effect: d.effect, Reason: "test"}, nil
}

func TestEligibilityAllows(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Allow}
	m, r := submitted(t, approval.Policy{Quorum: 1}, approval.WithDecider(d))

	got, err := m.Approve(context.Background(), r.ID, approval.Actor{
		Subject: access.Subject{ID: "bob", Roles: []string{"finance"}}})
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)

	assert.Equal(t, access.Action("payout.release:decide"), d.gotAction)
	assert.Equal(t, "bob", d.gotSubject.ID)
	assert.Equal(t, []string{"finance"}, d.gotSubject.Roles)
	assert.Equal(t, "approval", d.gotResource.Type)
	assert.Equal(t, r.ID.String(), d.gotResource.ID)
	assert.Equal(t, "payout.release", d.gotResource.Attrs["kind"])
	assert.Equal(t, "alice", d.gotResource.Attrs["requester"],
		"relational policies need the requester")

	raw, ok := d.gotResource.Attrs["payload"].(json.RawMessage)
	require.True(t, ok, "value-aware policies need the raw payload")
	assert.JSONEq(t, `{"payout_id":"po_88","amount":250000}`, string(raw))
}

func TestEligibilityDenies(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 1}, approval.WithDecider(d))

	_, err := m.Approve(context.Background(), r.ID, actor("dave"))
	assert.ErrorIs(t, err, approval.ErrNotEligible)

	got, err := m.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status, "a denied vote leaves no trace on the record")
	assert.Empty(t, got.Decisions)
}

func TestEligibilityFailsClosedOnDeciderError(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{err: errors.New("org chart unreachable")}
	m, r := submitted(t, approval.Policy{Quorum: 1}, approval.WithDecider(d))

	_, err := m.Approve(context.Background(), r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotEligible,
		"an unavailable decider must never grant approval")
}

func TestEligibilityGatesRejectToo(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	_, err := m.Reject(context.Background(), r.ID, actor("mallory"))
	assert.ErrorIs(t, err, approval.ErrNotEligible,
		"an ungated reject lets anyone DoS the approval queue")
	assert.Equal(t, access.Action("payout.release:decide"), d.gotAction)
}

func TestEligibilityRunsAfterCheapInvariants(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Allow}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	_, err := m.Approve(context.Background(), r.ID, actor("alice"))
	require.ErrorIs(t, err, approval.ErrSelfApproval)
	assert.Empty(t, d.gotAction, "self-approval is refused without consulting the decider")
}

func TestCancelUsesCancelVerbForNonRequester(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Allow}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	_, err := m.Cancel(context.Background(), r.ID, actor("ops-oncall"))
	require.NoError(t, err)
	assert.Equal(t, access.Action("payout.release:cancel"), d.gotAction)
}

func TestCancelByRequesterSkipsTheDecider(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	got, err := m.Cancel(context.Background(), r.ID, actor("alice"))
	require.NoError(t, err, "withdrawing your own request is never gated")
	assert.Equal(t, approval.Cancelled, got.Status)
	assert.Empty(t, d.gotAction)
}
