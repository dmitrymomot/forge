package redisqueue_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
	redisqueue "github.com/dmitrymomot/forge/async/queue/redis"
	"github.com/dmitrymomot/forge/core/id"
)

// runID makes every prefix unique per test process so re-runs against a
// persistent container (keys leak by design) never collide with prior state.
var runID = id.NewULID().String()

var _ queue.Broker = (*redisqueue.Broker)(nil)

func dial(tb testing.TB) redis.UniversalClient {
	tb.Helper()
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		tb.Skip("set FORGE_TEST_REDIS_URL (host:port)")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	tb.Cleanup(func() { _ = c.Close() })
	return c
}

var prefixSeq int

// newBroker namespaces each subtest under a unique prefix; keys leak into the
// ephemeral test container, which is acceptable.
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

func TestRedisQueue_ValidatesConstruction(t *testing.T) {
	t.Parallel()
	_, err := redisqueue.New(nil)
	require.Error(t, err)
}
