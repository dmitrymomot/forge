package approval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

// TestRetryReReadsFreshStateNotStaleCopy proves mutate's most load-bearing
// property deterministically: on a conflict retry, fn is re-run against a
// FRESH read rather than blindly reapplying the previous attempt's copy.
// carol's vote is committed for real, through a second Manager sharing the
// same underlying store, in the gap between bob's losing Update and his
// retry — the retry must observe it and merge with it, not overwrite it.
func TestRetryReReadsFreshStateNotStaleCopy(t *testing.T) {
	t.Parallel()
	p := approval.Policy{Quorum: 2}
	real := approval.NewMemoryStore()
	ctx := context.Background()
	m2 := approval.New(real, approval.WithKind(kindPayout, p), approval.WithClock(clock.NewMock(fixedNow)))

	r, err := approval.Submit(ctx, m2, kindPayout, payoutPayload{PayoutID: "po_88"},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	cs := &conflictStore{Store: real, left: 1, before: func() {
		_, err := m2.Approve(ctx, r.ID, actor("carol"))
		require.NoError(t, err)
	}}
	m := approval.New(cs, approval.WithKind(kindPayout, p), approval.WithClock(clock.NewMock(fixedNow)),
		approval.WithMaxRetries(5))

	got, err := m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status,
		"quorum is reached by carol's real vote plus bob's retried one")
	require.Len(t, got.Decisions, 2, "the retry must merge with carol's vote, not overwrite it")
	seen := map[string]bool{}
	for _, d := range got.Decisions {
		seen[d.Approver] = true
	}
	assert.True(t, seen["carol"] && seen["bob"], "both votes must survive")
	assert.Equal(t, 2, cs.calls, "one losing Update, one winning retry")
}

// TestDoubleVoteLosesRaceStillRejected closes the deferred finding: a
// same-approver double vote that loses the CAS race must be rejected with
// ErrAlreadyVoted on retry, never recorded a second time.
func TestDoubleVoteLosesRaceStillRejected(t *testing.T) {
	t.Parallel()
	p := approval.Policy{Quorum: 2}
	real := approval.NewMemoryStore()
	ctx := context.Background()
	m2 := approval.New(real, approval.WithKind(kindPayout, p), approval.WithClock(clock.NewMock(fixedNow)))

	r, err := approval.Submit(ctx, m2, kindPayout, payoutPayload{PayoutID: "po_88"},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	cs := &conflictStore{Store: real, left: 1, before: func() {
		// bob's own vote, committed for real, wins the race against his own
		// retried attempt below.
		_, err := m2.Approve(ctx, r.ID, actor("bob"))
		require.NoError(t, err)
	}}
	m := approval.New(cs, approval.WithKind(kindPayout, p), approval.WithClock(clock.NewMock(fixedNow)),
		approval.WithMaxRetries(5))

	_, err = m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrAlreadyVoted,
		"a vote that lost the race must be refused on retry, not recorded a second time")

	got, gerr := m2.Get(ctx, r.ID)
	require.NoError(t, gerr)
	require.Len(t, got.Decisions, 1, "bob's vote must be recorded exactly once")
	assert.Equal(t, "bob", got.Decisions[0].Approver)
}

// TestRetryExhaustionSurfacesErrConflict proves the previously-uncovered
// manager.go exhaustion branch: once maxRetries is used up, mutate returns
// ErrConflict rather than looping forever.
func TestRetryExhaustionSurfacesErrConflict(t *testing.T) {
	t.Parallel()
	p := approval.Policy{Quorum: 1}
	real := approval.NewMemoryStore()
	ctx := context.Background()
	cs := &conflictStore{Store: real, left: 100}
	m := approval.New(cs, approval.WithKind(kindPayout, p), approval.WithClock(clock.NewMock(fixedNow)),
		approval.WithMaxRetries(2))

	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{}, approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err, "Submit uses Create, not Update — unaffected by the injected conflicts")

	_, err = m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrConflict,
		"retries exhausted must surface ErrConflict rather than loop forever")
	assert.Equal(t, 3, cs.calls, "maxRetries=2 allows attempts 0, 1, and 2 — three Update calls total")
}

// TestMaxRetriesZeroIsSingleAttempt proves WithMaxRetries(0) means exactly
// one attempt: no retry after the first conflict.
func TestMaxRetriesZeroIsSingleAttempt(t *testing.T) {
	t.Parallel()
	p := approval.Policy{Quorum: 1}
	real := approval.NewMemoryStore()
	ctx := context.Background()
	cs := &conflictStore{Store: real, left: 1}
	m := approval.New(cs, approval.WithKind(kindPayout, p), approval.WithClock(clock.NewMock(fixedNow)),
		approval.WithMaxRetries(0))

	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{}, approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	_, err = m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrConflict)
	assert.Equal(t, 1, cs.calls, "WithMaxRetries(0) means a single attempt, no retry")
}

// There is intentionally no TestMaxRetriesNegativeClampsToZero here.
//
// mutate's exhaustion check is `case attempt >= m.cfg.maxRetries` with
// attempt starting at 0, so for ANY maxRetries <= 0 — clamped to 0 by
// options.go, or left raw at -5 without that clamp — the very first
// conflict already satisfies `0 >= maxRetries` and mutate stops after
// exactly one Update call. That holds for every possible number of
// injected conflicts, so no black-box assertion on call count, error, or
// outcome can ever distinguish "clamped to zero" from "left negative":
// grep confirms cfg.maxRetries is read nowhere else in the package. A
// test asserting this indistinguishable behavior would pass identically
// whether options.go's `if n < 0 { n = 0 }` clamp exists or not — proven
// by temporarily deleting that clamp and re-running the old version of
// this test, which still passed unchanged. The clamp itself stays (it is
// still the documented contract on WithMaxRetries and would matter the
// moment maxRetries is ever read from a second call site), but a test
// that cannot fail when the thing it guards is removed is worse than no
// test: it was deleted rather than kept as false assurance.
