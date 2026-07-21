package approval_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

var fixedNow = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

// newManager builds a Manager with a fixed clock and the payout kind.
func newManager(t *testing.T, p approval.Policy, extra ...approval.Option) *approval.Manager {
	t.Helper()
	opts := append([]approval.Option{
		approval.WithKind(kindPayout, p),
		approval.WithClock(clock.NewMock(fixedNow)),
	}, extra...)
	return approval.New(approval.NewMemoryStore(), opts...)
}

func TestSubmit(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "invoice #4471"})
	require.NoError(t, err)

	assert.False(t, r.ID.IsZero())
	assert.Equal(t, "payout.release", r.Kind)
	assert.Equal(t, "alice", r.Requester)
	assert.Equal(t, "invoice #4471", r.Reason)
	assert.Equal(t, approval.Pending, r.Status)
	assert.Equal(t, int64(1), r.Version)
	assert.Empty(t, r.Decisions)
	assert.True(t, fixedNow.Equal(r.CreatedAt))
	assert.True(t, fixedNow.Add(24*time.Hour).Equal(r.ExpiresAt))
}

func TestSubmitZeroTTLNeverExpires(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 1})
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_1"}, approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	assert.True(t, r.ExpiresAt.IsZero())
}

func TestSubmitRejectsEmptyRequester(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	_, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{}, approval.SubmitParams{})
	assert.ErrorIs(t, err, approval.ErrRequesterRequired)
}

func TestSubmitRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	unregistered := approval.NewKind[payoutPayload]("payout.unregistered")
	m := newManager(t, approval.Policy{Quorum: 2})
	_, err := approval.Submit(context.Background(), m, unregistered,
		payoutPayload{}, approval.SubmitParams{Requester: "alice"})
	assert.ErrorIs(t, err, approval.ErrUnknownKind)
}

func TestSubmitClonesMeta(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	meta := map[string]string{"request_id": "req_1"}

	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{}, approval.SubmitParams{Requester: "alice", Meta: meta})
	require.NoError(t, err)

	meta["request_id"] = "tampered"
	assert.Equal(t, "req_1", r.Meta["request_id"], "meta is cloned at submit")
}

// unmarshalablePayload cannot be encoded by encoding/json — a chan field
// always fails Marshal.
type unmarshalablePayload struct {
	Ch chan int
}

func TestSubmitMarshalFailureLeavesNothingPersisted(t *testing.T) {
	t.Parallel()
	kind := approval.NewKind[unmarshalablePayload]("test.submit-marshal-failure")
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kind, approval.Policy{Quorum: 1}),
		approval.WithClock(clock.NewMock(fixedNow)))
	ctx := context.Background()

	_, err := approval.Submit(ctx, m, kind, unmarshalablePayload{Ch: make(chan int)},
		approval.SubmitParams{Requester: "alice"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal payload")
	var jsonErr *json.UnsupportedTypeError
	assert.ErrorAs(t, err, &jsonErr, "the underlying json error must surface, not be swallowed")

	got, lerr := m.List(ctx, approval.Filter{Kind: kind.Name()})
	require.NoError(t, lerr)
	assert.Empty(t, got, "a marshal failure must abort before Store.Create — nothing may be persisted")
}

func TestPayloadOf(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	want := payoutPayload{PayoutID: "po_88", Amount: 250000}

	r, err := approval.Submit(context.Background(), m, kindPayout, want,
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	got, err := approval.PayloadOf(kindPayout, r)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestPayloadOfRejectsWrongKind(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_88"}, approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	other := approval.NewKind[struct{ Days int }]("hr.vacation")
	_, err = approval.PayloadOf(other, r)
	assert.ErrorIs(t, err, approval.ErrKindMismatch)
}
