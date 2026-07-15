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
	t.Run("ClaimOrder", func(t *testing.T) { testClaimOrder(t, factory(t)) })
	t.Run("NackReschedules", func(t *testing.T) { testNackReschedules(t, factory(t)) })
	t.Run("DelayedJob", func(t *testing.T) { testDelayedJob(t, factory(t)) })
	t.Run("LeaseExpiryRedelivery", func(t *testing.T) { testLeaseExpiry(t, factory(t)) })
	t.Run("ExtendPreventsRedelivery", func(t *testing.T) { testExtend(t, factory(t)) })
	t.Run("DeadLetterOps", func(t *testing.T) { testDeadLetterOps(t, factory(t)) })
	t.Run("QueueIsolation", func(t *testing.T) { testQueueIsolation(t, factory(t)) })
	t.Run("Stats", func(t *testing.T) { testStats(t, factory(t)) })
	t.Run("ClaimEmptyQueue", func(t *testing.T) { testClaimEmpty(t, factory(t)) })
}

func makeJob(q string, runAt time.Time) queue.Job {
	return queue.Job{
		ID:          id.NewULID().String(),
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
func claimWithin(t *testing.T, b queue.Broker, q string, max int, lease time.Duration, want int) []queue.Job {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := b.Claim(ctx, q, max, lease)
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

func claimIDs(jobs []queue.Job) []string {
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

	again, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again, "claimed job must be invisible during lease")

	require.NoError(t, b.Ack(ctx, j.ID))
	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, st["q1"].Pending)
	assert.Zero(t, st["q1"].Dead)
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

	require.NoError(t, b.Nack(ctx, j.ID, time.Now().Add(250*time.Millisecond), "boom"))

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
	require.NoError(t, b.Extend(ctx, j.ID, 2*time.Second))

	time.Sleep(300 * time.Millisecond)                        // past the original lease, inside the extended one
	still, err := b.Claim(ctx, "q1", 1, 400*time.Millisecond) // same lease as the original claim (see LeaseExpiry note)
	require.NoError(t, err)
	assert.Empty(t, still, "extended lease must prevent redelivery")

	require.NoError(t, b.Ack(ctx, j.ID))
}

func testDeadLetterOps(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	j1 := makeJob("q1", time.Now().Add(-2*time.Second))
	j2 := makeJob("q1", time.Now().Add(-time.Second))
	require.NoError(t, b.Push(ctx, j1))
	require.NoError(t, b.Push(ctx, j2))

	got, err := b.Claim(ctx, "q1", 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.NoError(t, b.Kill(ctx, j1.ID, "poison"))
	require.NoError(t, b.Kill(ctx, j2.ID, "poison"))

	dead, err := b.ListDead(ctx, "q1", 10)
	require.NoError(t, err)
	require.Len(t, dead, 2)
	assert.Equal(t, "poison", dead[0].LastError)

	one, err := b.ListDead(ctx, "q1", 1)
	require.NoError(t, err)
	assert.Len(t, one, 1, "ListDead must honor limit")

	// Requeue resets attempts and makes the job claimable again.
	require.NoError(t, b.Requeue(ctx, j1.ID))
	re, err := b.Claim(ctx, "q1", 10, time.Minute)
	require.NoError(t, err)
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

	assert.ErrorIs(t, b.Purge(ctx, "no-such-id"), queue.ErrJobNotFound)
	assert.ErrorIs(t, b.Requeue(ctx, "no-such-id"), queue.ErrJobNotFound)
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
	require.NoError(t, b.Push(ctx, j1))
	require.NoError(t, b.Push(ctx, j2))

	got := claimWithin(t, b, "q1", 1, time.Minute, 1)
	require.NoError(t, b.Kill(ctx, got[0].ID, "x"))

	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["q1"].Pending)
	assert.Equal(t, 1, st["q1"].Dead)
}

func testClaimEmpty(t *testing.T, b queue.Broker) {
	ctx := context.Background()
	got, err := b.Claim(ctx, "nothing-here", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got)
}
