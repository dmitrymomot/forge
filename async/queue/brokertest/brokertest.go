// Package brokertest is the executable contract for queue.Broker
// implementations. Every driver's test suite must call Run; the in-memory
// broker is the reference implementation. Timing subtests use short real
// leases (hundreds of ms) and poll for the expected outcome rather than
// asserting once after a fixed sleep, so the suite tolerates clock skew
// between the test process and a containerised database (e.g. a Docker VM
// clock that lags under load) and is safe for live backends.
package brokertest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

// Run executes the Broker conformance suite. factory must return a fresh,
// empty broker (or one namespaced per test) each call.
func Run(t *testing.T, factory func(t *testing.T) queue.Broker) {
	t.Helper()
	t.Run("PushClaimAck", func(t *testing.T) { testPushClaimAck(t, factory(t)) })
	t.Run("BatchPush", func(t *testing.T) { testBatchPush(t, factory(t)) })
	t.Run("ClaimOrder", func(t *testing.T) { testClaimOrder(t, factory(t)) })
	t.Run("NackReschedules", func(t *testing.T) { testNackReschedules(t, factory(t)) })
	t.Run("DelayedJob", func(t *testing.T) { testDelayedJob(t, factory(t)) })
	t.Run("LeaseExpiryRedelivery", func(t *testing.T) { testLeaseExpiry(t, factory(t)) })
	t.Run("ExtendPreventsRedelivery", func(t *testing.T) { testExtend(t, factory(t)) })
	t.Run("Fencing", func(t *testing.T) { testFencing(t, factory(t)) })
	t.Run("DeadLetterOps", func(t *testing.T) { testDeadLetterOps(t, factory(t)) })
	t.Run("DeadOrderedByKillTime", func(t *testing.T) { testDeadOrder(t, factory(t)) })
	t.Run("PurgeDeadBefore", func(t *testing.T) { testPurgeDeadBefore(t, factory(t)) })
	t.Run("QueueIsolation", func(t *testing.T) { testQueueIsolation(t, factory(t)) })
	t.Run("Stats", func(t *testing.T) { testStats(t, factory(t)) })
	t.Run("ClaimEmptyQueue", func(t *testing.T) { testClaimEmpty(t, factory(t)) })
}

func makeJob(q string, runAt time.Time) queue.Job {
	return queue.Job{
		ID:          id.NewUUID().String(),
		Queue:       q,
		Type:        "test.kind",
		Payload:     []byte(`{"n":1}`),
		MaxAttempts: 25,
		RunAt:       runAt.UTC(),
		CreatedAt:   time.Now().UTC(),
	}
}

// dueNow returns a RunAt that is already in the past by a wide margin. RunAt is
// stamped from the test-process clock but visibility is decided by the database
// clock; biasing "claimable now" jobs into the past keeps them claimable even
// when the database clock lags the test process (e.g. a Docker VM under load).
func dueNow() time.Time { return time.Now().Add(-2 * time.Second) }

// claimWithin polls Claim until it returns at least want jobs or the deadline
// passes, tolerating clock skew and slow containers. It returns the claimed
// batch; on timeout it makes a final assertion so the failure names the queue.
func claimWithin(t *testing.T, b queue.Broker, q string, limit int, lease time.Duration, want int) []queue.ClaimedJob {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := b.Claim(ctx, q, limit, lease)
		require.NoError(t, err)
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			require.Len(t, got, want, "queue %q: expected %d claimable job(s) within deadline", q, want)
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func claimIDs(jobs []queue.ClaimedJob) []string {
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids
}

func testPushClaimAck(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 10, time.Minute, 1)
	assert.Equal(t, j.ID, got[0].ID)
	assert.Equal(t, j.Type, got[0].Type)
	assert.JSONEq(t, string(j.Payload), string(got[0].Payload))
	assert.Equal(t, 1, got[0].Attempt, "claim must increment attempt")
	assert.Equal(t, j.MaxAttempts, got[0].MaxAttempts)
	assert.NotEmpty(t, got[0].Token, "claim must stamp a fencing token")

	again, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again, "claimed job must be invisible during lease")

	require.NoError(t, b.Ack(ctx, j.ID, got[0].Token))
	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, st["q1"].Pending)
	assert.Zero(t, st["q1"].Dead)

	require.NoError(t, b.Push(ctx), "empty batch push is a no-op")
}

func testBatchPush(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	now := time.Now()
	j1 := makeJob("q1", now.Add(-3*time.Second))
	j2 := makeJob("q1", now.Add(-2*time.Second))
	j3 := makeJob("q1", now.Add(-1*time.Second))
	require.NoError(t, b.Push(ctx, j1, j2, j3))

	got := claimWithin(t, b, "q1", 10, time.Minute, 3)
	assert.Equal(t, []string{j1.ID, j2.ID, j3.ID}, claimIDs(got), "batch push claims back in (run_at, id) order")
}

func testClaimOrder(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	now := time.Now()
	a := makeJob("q1", now.Add(-2*time.Second))
	c := makeJob("q1", now.Add(-time.Second))
	require.NoError(t, b.Push(ctx, a))
	require.NoError(t, b.Push(ctx, c))

	got, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{a.ID, c.ID}, claimIDs(got), "earlier run_at claims first (best-effort FIFO)")
}

func testNackReschedules(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 1, time.Minute, 1)
	require.Len(t, got, 1)

	require.NoError(t, b.Nack(ctx, j.ID, got[0].Token, time.Now().Add(250*time.Millisecond), "boom"))

	early, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, early, "nacked job must stay invisible until retryAt")

	late := claimWithin(t, b, "q1", 1, time.Minute, 1)
	assert.Equal(t, j.ID, late[0].ID)
	assert.Equal(t, 2, late[0].Attempt, "second claim = attempt 2")
	assert.Equal(t, "boom", late[0].LastError)
}

func testDelayedJob(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", time.Now().Add(300*time.Millisecond))
	require.NoError(t, b.Push(ctx, j))

	got, err := b.Claim(ctx, "q1", 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "future job must not be claimable")

	late := claimWithin(t, b, "q1", 1, time.Minute, 1)
	assert.Equal(t, j.ID, late[0].ID)
}

func testLeaseExpiry(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	require.Len(t, got, 1)

	early, err := b.Claim(ctx, "q1", 1, 300*time.Millisecond)
	require.NoError(t, err)
	assert.Empty(t, early)

	// Reclaim with the SAME lease: some drivers (redis) enforce expiry at
	// claim time via min-idle = the claiming lease, so a longer lease here
	// would hide the expiry. Poll so a lagging database clock only slows the
	// redelivery rather than failing the assertion.
	late := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	assert.Equal(t, j.ID, late[0].ID)
	assert.Equal(t, 2, late[0].Attempt)
}

func testExtend(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	got := claimWithin(t, b, "q1", 1, 400*time.Millisecond, 1)
	require.Len(t, got, 1)

	time.Sleep(250 * time.Millisecond)
	require.NoError(t, b.Extend(ctx, j.ID, got[0].Token, 2*time.Second))

	time.Sleep(300 * time.Millisecond)                        // past the original lease, inside the extended one
	still, err := b.Claim(ctx, "q1", 1, 400*time.Millisecond) // same lease as the original claim (see LeaseExpiry note)
	require.NoError(t, err)
	assert.Empty(t, still, "extended lease must prevent redelivery")

	require.NoError(t, b.Ack(ctx, j.ID, got[0].Token))
}

func testFencing(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))

	first := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	require.Len(t, first, 1)
	stale := first[0].Token

	// Let the lease expire and reclaim: the second claim owns the job now.
	second := claimWithin(t, b, "q1", 1, 300*time.Millisecond, 1)
	require.Len(t, second, 1)
	require.Equal(t, j.ID, second[0].ID)

	// Every fenced op with the stale token must refuse and leave the new
	// claim's state alone.
	assert.ErrorIs(t, b.Extend(ctx, j.ID, stale, time.Minute), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Ack(ctx, j.ID, stale), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Nack(ctx, j.ID, stale, time.Now(), "stale"), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Kill(ctx, j.ID, stale, "stale"), queue.ErrLeaseLost)

	// The job is still claimed by the second owner: invisible, not dead.
	invisible, err := b.Claim(ctx, "q1", 1, 300*time.Millisecond)
	require.NoError(t, err)
	assert.Empty(t, invisible, "stale-token ops must not release or kill the current claim")
	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)

	// Fenced ops on an unknown id are also ErrLeaseLost, never ErrJobNotFound.
	missingID := id.NewUUID().String()
	assert.ErrorIs(t, b.Ack(ctx, missingID, stale), queue.ErrLeaseLost)
	assert.ErrorIs(t, b.Extend(ctx, missingID, stale, time.Minute), queue.ErrLeaseLost)

	// The live token still works.
	require.NoError(t, b.Ack(ctx, j.ID, second[0].Token))
}

func testDeadLetterOps(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now().Add(-2*time.Second))
	j2 := makeJob("q1", time.Now().Add(-time.Second))
	require.NoError(t, b.Push(ctx, j1, j2))

	got, err := b.Claim(ctx, "q1", 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.NoError(t, b.Kill(ctx, got[0].ID, got[0].Token, "poison"))
	require.NoError(t, b.Kill(ctx, got[1].ID, got[1].Token, "poison"))

	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 2)
	assert.Equal(t, "poison", dead[0].LastError)

	one, err := b.ListDead(ctx, "q1", 1)
	require.NoError(t, err)
	assert.Len(t, one, 1, "ListDead must honor limit")

	// Requeue resets attempts and makes the job claimable again.
	require.NoError(t, b.Requeue(ctx, j1.ID))
	re := claimWithin(t, b, "q1", 10, time.Minute, 1)
	require.Len(t, re, 1)
	assert.Equal(t, j1.ID, re[0].ID)
	assert.Equal(t, 1, re[0].Attempt, "requeue must reset attempts")

	// Requeue on a non-dead job fails.
	assert.ErrorIs(t, b.Requeue(ctx, j1.ID), queue.ErrNotDead)

	// Purge removes the dead job.
	require.NoError(t, b.Purge(ctx, j2.ID))
	dead, err = b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)

	missingID := id.NewUUID().String()
	assert.ErrorIs(t, b.Purge(ctx, missingID), queue.ErrJobNotFound)
	assert.ErrorIs(t, b.Requeue(ctx, missingID), queue.ErrJobNotFound)
}

func testDeadOrder(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now().Add(-2*time.Second))
	j2 := makeJob("q1", time.Now().Add(-time.Second))
	require.NoError(t, b.Push(ctx, j1, j2))

	got := claimWithin(t, b, "q1", 2, time.Minute, 2)
	require.Len(t, got, 2)
	byID := map[string]queue.ClaimedJob{got[0].ID: got[0], got[1].ID: got[1]}

	// Kill j2 first, j1 second, with a gap wide enough that kill timestamps
	// differ even at millisecond resolution.
	require.NoError(t, b.Kill(ctx, j2.ID, byID[j2.ID].Token, "first-death"))
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, b.Kill(ctx, j1.ID, byID[j1.ID].Token, "second-death"))

	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 2)
	assert.Equal(t, j2.ID, dead[0].ID, "ListDead orders by kill time, not creation or run order")
	assert.Equal(t, j1.ID, dead[1].ID)
}

func testPurgeDeadBefore(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j))
	got := claimWithin(t, b, "q1", 1, time.Minute, 1)
	require.NoError(t, b.Kill(ctx, j.ID, got[0].Token, "x"))

	// A cutoff far in the past removes nothing (skew-proof bounds: even a
	// lagging database clock is within ±1h of the test process).
	n, err := b.PurgeDeadBefore(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)

	// A cutoff far in the future removes the dead job and reports the count.
	n, err = b.PurgeDeadBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	dead, err = b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)
}

func testQueueIsolation(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", dueNow())
	j2 := makeJob("q2", dueNow())
	require.NoError(t, b.Push(ctx, j1))
	require.NoError(t, b.Push(ctx, j2))

	got := claimWithin(t, b, "q1", 10, time.Minute, 1)
	assert.Equal(t, j1.ID, got[0].ID)
}

func testStats(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", dueNow())
	j2 := makeJob("q1", dueNow())
	require.NoError(t, b.Push(ctx, j1, j2))

	got := claimWithin(t, b, "q1", 1, time.Minute, 1)
	require.NoError(t, b.Kill(ctx, got[0].ID, got[0].Token, "x"))

	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["q1"].Pending)
	assert.Equal(t, 1, st["q1"].Dead)
	assert.False(t, st["q1"].PendingCapped, "counts this small are never capped")
	assert.False(t, st["q1"].DeadCapped)
}

func testClaimEmpty(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	got, err := b.Claim(ctx, "nothing-here", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got)
}
