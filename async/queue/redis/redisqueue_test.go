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
	require.NoError(t, b2.Ack(ctx, got2[0].ID))
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
	require.NoError(t, b2.Nack(ctx, crashed[0].ID, time.Now(), "still failing"))

	time.Sleep(20 * time.Millisecond)
	retried, err := b2.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, retried, 1)
	assert.Equal(t, 3, retried[0].Attempt, "post-nack claim must continue from the crash-redelivered attempt, not reset")
	assert.Equal(t, "still failing", retried[0].LastError)
	require.NoError(t, b2.Ack(ctx, retried[0].ID))
}
