//go:build integration

package redisqueue_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
	redisqueue "github.com/dmitrymomot/forge/async/queue/redis"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

// runID makes every prefix unique per test process so re-runs against a
// persistent server (keys leak by design) never collide with prior state.
var runID = id.NewULID().String()

var _ queue.Broker = (*redisqueue.Broker)(nil)

// dial connects to the redis under test: redistest.Addr honors
// FORGE_TEST_REDIS_URL if set, else starts a throwaway container shared across
// the test process.
func dial(tb testing.TB) redis.UniversalClient {
	tb.Helper()
	c := redis.NewClient(&redis.Options{Addr: redistest.Addr(tb)})
	tb.Cleanup(func() { _ = c.Close() })
	return c
}

var prefixSeq int

// newBroker namespaces each subtest under a unique prefix; keys leak into the
// ephemeral test server (the redistest container or a shared real redis), which
// is acceptable.
func newBroker(tb testing.TB) *redisqueue.Broker {
	tb.Helper()
	prefixSeq++
	b, err := redisqueue.New(dial(tb), redisqueue.WithPrefix(fmt.Sprintf("qt:%s:%s:%d:", runID, tb.Name(), prefixSeq)))
	require.NoError(tb, err)
	return b
}

func TestRedisQueue_Conformance(t *testing.T) {
	brokertest.Run(t, func(t *testing.T) queue.Broker { return newBroker(t) })
}

func TestRedisQueue_NoTxPusher(t *testing.T) {
	b := newBroker(t)
	c := queue.NewClient(b)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("tx.kind")
	err := queue.PushTx(context.Background(), c, "tx", kind, struct {
		N int `json:"n"`
	}{N: 1})
	assert.ErrorIs(t, err, queue.ErrTxUnsupported)
}

// claimAllWithin polls Claim until it has collected want jobs, accumulating
// partial batches. Each Claim call mints its own token, so callers must index
// tokens by job id rather than assume one token for the whole slice.
// Redelivery is gated by the redis server's idle clock, so poll for the
// expected outcome instead of asserting once after a fixed sleep.
func claimAllWithin(tb testing.TB, b *redisqueue.Broker, q string, want int, lease time.Duration) []queue.ClaimedJob {
	tb.Helper()
	ctx := context.Background()
	got := make([]queue.ClaimedJob, 0, want)
	deadline := time.Now().Add(3 * time.Second)
	for len(got) < want {
		batch, err := b.Claim(ctx, q, want-len(got), lease)
		require.NoError(tb, err)
		got = append(got, batch...)
		if len(got) >= want {
			break
		}
		require.False(tb, time.Now().After(deadline), "claimed %d of %d job(s) from %q within deadline", len(got), want, q)
		time.Sleep(25 * time.Millisecond)
	}
	return got
}

// tokensByID indexes claim tokens by job id.
func tokensByID(jobs []queue.ClaimedJob) map[string]string {
	m := make(map[string]string, len(jobs))
	for _, j := range jobs {
		m[j.ID] = j.Token
	}
	return m
}

// TestRedisQueue_StaleWorkerCannotClobberReclaimedJob covers the Lua
// PEL-ownership check inside the finalize scripts — the layer no other test
// reaches. brokertest's Fencing subtest drives a SINGLE broker instance, so its
// stale-token ops are rejected in-process by take() and the scripts never run;
// the crash tests kill b1 before it ever attempts a finalize.
//
// Here b1 stays LIVE and keeps its refs, so its token passes take() and the
// scripts DO run: only the XPENDING consumer match stands between b1 and
// clobbering the job b2 reclaimed once the lease expired. One job per op,
// because take() removes the ref on a token match — reusing a single job would
// send every op after the first down the in-process path instead of the Lua one.
func TestRedisQueue_StaleWorkerCannotClobberReclaimedJob(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:stale:%s:", runID)
	b1, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	ctx := context.Background()

	const lease = 300 * time.Millisecond

	c := queue.NewClient(b1)
	for range 5 { // one job per fenced op, plus a control
		require.NoError(t, c.PushRaw(ctx, "stale.kind", []byte(`{}`)))
	}

	claimed := claimAllWithin(t, b1, "default", 5, lease)
	b1Tok := tokensByID(claimed)
	require.Len(t, b1Tok, 5)
	ackID, killID, nackID, extendID, controlID := claimed[0].ID, claimed[1].ID, claimed[2].ID, claimed[3].ID, claimed[4].ID

	// Premise guard: while b1 still owns the PEL entries its token finalizes for
	// real. Without this the assertions below could pass vacuously on a take()
	// miss instead of on the Lua check they exist to cover.
	require.NoError(t, b1.Ack(ctx, controlID, b1Tok[controlID]), "b1's token must be live before the lease expires")

	// b1 does NOT crash: it keeps its refs, so take() still matches its token.
	b2, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	time.Sleep(lease + 100*time.Millisecond) // let b1's entries go idle

	reclaimed := claimAllWithin(t, b2, "default", 4, lease)
	b2Tok := tokensByID(reclaimed)
	require.Len(t, b2Tok, 4, "b2 must reclaim every expired job exactly once")

	// Every fenced op from the live-but-stale b1 must be refused by the script's
	// ownership check — take() cannot catch these, the token is b1's own.
	assert.ErrorIs(t, b1.Ack(ctx, ackID, b1Tok[ackID]), queue.ErrLeaseLost)
	assert.ErrorIs(t, b1.Kill(ctx, killID, b1Tok[killID], "stale kill"), queue.ErrLeaseLost)
	assert.ErrorIs(t, b1.Nack(ctx, nackID, b1Tok[nackID], time.Now(), "stale nack"), queue.ErrLeaseLost)
	assert.ErrorIs(t, b1.Extend(ctx, extendID, b1Tok[extendID], time.Minute), queue.ErrLeaseLost)

	dead, err := b2.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	assert.Empty(t, dead, "a stale worker's Kill must not dead-letter a job it no longer owns")

	// b2's claim is undisturbed: its own token still finalizes every job.
	for jobID, tok := range b2Tok {
		assert.NoError(t, b2.Ack(ctx, jobID, tok), "b2's claim must survive the stale worker's ops")
	}
}

func TestRedisQueue_AttemptSurvivesCrashRedelivery(t *testing.T) {
	// A second Broker instance (fresh refs — simulated crash) must still see
	// the correct attempt count via the XPENDING delivery counter.
	client := dial(t)
	prefix := fmt.Sprintf("qt:crash:%d:", time.Now().UnixNano())
	b1, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	ctx := context.Background()

	c := queue.NewClient(b1)
	require.NoError(t, c.PushRaw(ctx, "crash.kind", []byte(`{}`)))

	got, err := b1.Claim(ctx, "default", 1, 200*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].Attempt)
	// b1 "crashes": no Ack, refs lost.

	b2, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	got2, err := b2.Claim(ctx, "default", 1, 200*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got2, 1)
	assert.Equal(t, 2, got2[0].Attempt, "redelivery after crash must count as a new attempt")
	require.NoError(t, b2.Ack(ctx, got2[0].ID, got2[0].Token))
}

func TestRedisQueue_NackAfterCrashRedeliveryPreservesAttempts(t *testing.T) {
	// A crash-redelivered claim (XAUTOCLAIM, delivered >= 2) that ends in an
	// explicit Nack must persist the consumed attempt, so the next claim keeps
	// counting up instead of resetting — otherwise a flaky job outlives its
	// MaxAttempts budget.
	client := dial(t)
	prefix := fmt.Sprintf("qt:nackcrash:%s:", runID)
	b1, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	ctx := context.Background()

	c := queue.NewClient(b1)
	require.NoError(t, c.PushRaw(ctx, "flaky.kind", []byte(`{}`)))

	got, err := b1.Claim(ctx, "default", 1, 200*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].Attempt, "first claim = attempt 1")
	// b1 "crashes": no Ack/Nack, refs lost.

	b2, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond) // lease expires

	crashed, err := b2.Claim(ctx, "default", 1, 200*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, crashed, 1)
	assert.Equal(t, 2, crashed[0].Attempt, "crash redelivery = attempt 2")

	// Handler fails → Nack. The consumed attempt (2) must survive.
	require.NoError(t, b2.Nack(ctx, crashed[0].ID, crashed[0].Token, time.Now(), "still failing"))

	time.Sleep(20 * time.Millisecond)
	retried, err := b2.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, retried, 1)
	assert.Equal(t, 3, retried[0].Attempt, "post-nack claim must continue from the crash-redelivered attempt, not reset")
	assert.Equal(t, "still failing", retried[0].LastError)
	require.NoError(t, b2.Ack(ctx, retried[0].ID, retried[0].Token))
}

// TestRedisQueue_PurgeDeadBeforeSpansManyBatches covers the batched drain loop
// that brokertest's PurgeDeadBefore subtest cannot reach: it purges a single
// dead job, so one exec always suffices and an off-by-one in the loop — a
// batch silently left behind, or a miscounted total — would go unnoticed.
// 1200 dead jobs span three purgeBatch(500) execs with a short final one.
func TestRedisQueue_PurgeDeadBeforeSpansManyBatches(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:purgebatch:%s:", runID)
	ctx := context.Background()

	b, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)

	const dead = 1200 // > 2x the 500 batch: two full batches plus a short one
	c := queue.NewClient(b)
	for range dead {
		require.NoError(t, c.PushRaw(ctx, "purge.kind", []byte(`{}`)))
	}
	claimed := claimAllWithin(t, b, "default", dead, time.Minute)
	require.Len(t, claimed, dead)
	for _, cj := range claimed {
		require.NoError(t, b.Kill(ctx, cj.ID, cj.Token, "retention fixture"))
	}

	// Premise guard: without this, an assertion of "everything is gone" could
	// pass on a DLQ that was never populated.
	require.Equal(t, int64(dead), client.HLen(ctx, prefix+"default:dead").Val(), "fixture must fill the dead hash")
	require.Equal(t, int64(dead), client.ZCard(ctx, prefix+"default:dead:idx").Val(), "fixture must fill the dead index")

	n, err := b.PurgeDeadBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, dead, n, "the batched loop must report every purged job exactly once across all batches")

	// Both structures must be empty: a loop that stops one batch early leaves
	// entries behind in BOTH, and one that only cleans the index leaves the
	// hash growing forever.
	assert.Zero(t, client.HLen(ctx, prefix+"default:dead").Val(), "no dead envelope may survive the sweep")
	assert.Zero(t, client.ZCard(ctx, prefix+"default:dead:idx").Val(), "no dead index entry may survive the sweep")
	left, err := b.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	assert.Empty(t, left)

	// A second sweep over an already-drained DLQ is a no-op, not an infinite
	// loop: the first exec returns a short batch and breaks.
	n, err = b.PurgeDeadBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
}

var _ queue.Maintainer = (*redisqueue.Broker)(nil)

func TestRedisQueue_PoisonEntryDoesNotWedgeClaim(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:poison:%s:", runID)
	b, err := redisqueue.New(client, redisqueue.WithPrefix(prefix))
	require.NoError(t, err)
	ctx := context.Background()
	c := queue.NewClient(b)

	// Establish the stream+group, drain it.
	require.NoError(t, c.PushRaw(ctx, "ok.kind", []byte(`{}`)))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b.Ack(ctx, got[0].ID, got[0].Token))

	// A foreign producer XADDs garbage straight into the stream.
	require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{
		Stream: prefix + "default", Values: map[string]any{"j": "not json at all"},
	}).Err())
	require.NoError(t, c.PushRaw(ctx, "ok.kind", []byte(`{"n":2}`)))

	// Claim parks the poison entry and still returns the good job.
	good, err := b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, good, 1)
	assert.Equal(t, "ok.kind", good[0].Type)
	require.NoError(t, b.Ack(ctx, good[0].ID, good[0].Token))

	parked, err := client.LRange(ctx, prefix+"default:poison", 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, parked, 1)
	assert.Equal(t, "not json at all", parked[0])

	// The queue is fully drained: subsequent claims stay clean.
	again, err := b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again)
}

func TestRedisQueue_MaintainCleansConsumersAndStaleQueues(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:maintain:%s:", runID)
	ctx := context.Background()

	// b1 processes a job and "retires" (its consumer entry lingers).
	b1, err := redisqueue.New(client, redisqueue.WithPrefix(prefix), redisqueue.WithConsumerIdleCutoff(time.Millisecond))
	require.NoError(t, err)
	c := queue.NewClient(b1)
	require.NoError(t, c.PushRaw(ctx, "m.kind", []byte(`{}`), queue.WithQueue("tmpq")))
	got, err := b1.Claim(ctx, "tmpq", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b1.Ack(ctx, got[0].ID, got[0].Token))

	// b2 maintains: b1's consumer is idle with zero pending → deleted; tmpq is
	// completely empty → dropped from the queues registry.
	b2, err := redisqueue.New(client, redisqueue.WithPrefix(prefix), redisqueue.WithConsumerIdleCutoff(time.Millisecond))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond) // let b1's consumer idle past the cutoff
	require.NoError(t, b2.Maintain(ctx))

	consumers, err := client.XInfoConsumers(ctx, prefix+"tmpq", "workers").Result()
	require.NoError(t, err)
	for _, cons := range consumers {
		assert.NotZero(t, cons.Pending, "zero-pending idle consumers must be deleted (only b2's own live consumer may remain)")
	}

	members, err := client.SMembers(ctx, prefix+"queues").Result()
	require.NoError(t, err)
	assert.NotContains(t, members, "tmpq", "fully-empty queue must leave the registry")

	// A later push simply re-registers the queue.
	require.NoError(t, c.PushRaw(ctx, "m.kind", []byte(`{}`), queue.WithQueue("tmpq")))
	st, err := b2.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["tmpq"].Pending)
}

// TestRedisQueue_MaintainSkipsBusyConsumer covers the guard that
// TestRedisQueue_MaintainCleansConsumersAndStaleQueues never reaches: a
// consumer with a pending (claimed but not finalized) entry must survive
// Maintain even when idle past the cutoff. bBusy claims the job and never
// finalizes it, so its consumer carries Pending > 0 the whole test; bMaint is
// a separate instance so bBusy is never Maintain's own caller, isolating the
// busy guard from the self guard. Without the guard (or with the read-then-
// delete race from finding 1), Maintain would XGROUP DELCONSUMER bBusy's
// consumer, discarding its PEL entry outright — the claimed job would then be
// stuck in the raw stream, invisible to the group, unable to Ack.
func TestRedisQueue_MaintainSkipsBusyConsumer(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:maintainbusy:%s:", runID)
	ctx := context.Background()

	bBusy, err := redisqueue.New(client, redisqueue.WithPrefix(prefix), redisqueue.WithConsumerIdleCutoff(time.Millisecond))
	require.NoError(t, err)
	c := queue.NewClient(bBusy)
	require.NoError(t, c.PushRaw(ctx, "busy.kind", []byte(`{}`), queue.WithQueue("busyq")))
	got, err := bBusy.Claim(ctx, "busyq", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	// No Ack: the job stays claimed, so bBusy's consumer keeps Pending == 1.

	bMaint, err := redisqueue.New(client, redisqueue.WithPrefix(prefix), redisqueue.WithConsumerIdleCutoff(time.Millisecond))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond) // bBusy's consumer goes idle past the cutoff too
	require.NoError(t, bMaint.Maintain(ctx))

	consumers, err := client.XInfoConsumers(ctx, prefix+"busyq", "workers").Result()
	require.NoError(t, err)
	var busyStillPresent bool
	for _, cons := range consumers {
		if cons.Pending > 0 {
			busyStillPresent = true
		}
	}
	assert.True(t, busyStillPresent, "a consumer with a pending entry must survive Maintain even when idle past the cutoff")

	// The in-flight job must still be finalizable: Maintain must not have
	// discarded its PEL entry out from under it.
	require.NoError(t, bBusy.Ack(ctx, got[0].ID, got[0].Token), "job claimed before Maintain must still be finalizable after it")
}

// TestRedisQueue_MaintainSkipsSelfConsumer covers the guard that a broker
// never deletes its own consumer registration, even when it is (by
// construction, since it just Acked) zero-pending and idle past the cutoff.
// Without the guard, a long-running worker that calls its own Maintain would
// wipe its own consumer entry from the group.
func TestRedisQueue_MaintainSkipsSelfConsumer(t *testing.T) {
	client := dial(t)
	prefix := fmt.Sprintf("qt:maintainself:%s:", runID)
	ctx := context.Background()

	b, err := redisqueue.New(client, redisqueue.WithPrefix(prefix), redisqueue.WithConsumerIdleCutoff(time.Millisecond))
	require.NoError(t, err)
	c := queue.NewClient(b)
	require.NoError(t, c.PushRaw(ctx, "self.kind", []byte(`{}`), queue.WithQueue("selfq")))
	got, err := b.Claim(ctx, "selfq", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b.Ack(ctx, got[0].ID, got[0].Token)) // zero-pending: only the self guard protects it now

	before, err := client.XInfoConsumers(ctx, prefix+"selfq", "workers").Result()
	require.NoError(t, err)
	require.Len(t, before, 1, "only b's own consumer should exist yet")

	time.Sleep(50 * time.Millisecond) // idle past the cutoff
	require.NoError(t, b.Maintain(ctx))

	after, err := client.XInfoConsumers(ctx, prefix+"selfq", "workers").Result()
	require.NoError(t, err)
	require.Len(t, after, 1, "a broker's own live consumer must survive its own Maintain call")
	assert.Equal(t, before[0].Name, after[0].Name)
}
