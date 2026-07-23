package fanout_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/realtime/fanout"
)

// recv pulls the next message or fails the test after a timeout.
func recv(t *testing.T, sub *fanout.Subscription) fanout.Message {
	t.Helper()
	select {
	case m, ok := <-sub.C():
		require.True(t, ok, "subscription channel closed")
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
		return fanout.Message{}
	}
}

// expectClosed asserts the channel closes (draining pending messages) within
// the timeout.
func expectClosed(t *testing.T, sub *fanout.Subscription) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-sub.C():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for channel close")
		}
	}
}

func TestMultiTopicDeliveryMonotonic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	hub, err := fanout.New()
	require.NoError(t, err)
	defer hub.Close()

	const perTopic = 500
	// Buffer larger than the total so nothing is dropped and every published ID
	// is observed.
	sub, err := hub.Subscribe(ctx, []string{"a", "b"}, fanout.WithBuffer(4*perTopic))
	require.NoError(t, err)
	defer sub.Close()

	var wg sync.WaitGroup
	for _, topic := range []string{"a", "b"} {
		wg.Go(func() {
			for range perTopic {
				if err := hub.Publish(ctx, topic, []byte("x")); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()

	// A subscriber multiplexing both topics must observe the global ID sequence
	// strictly increasing; the SSE Last-Event-ID cursor depends on it. IDs are
	// allocated globally but topics lock independently, so without ordering a
	// higher ID could be enqueued ahead of a lower one.
	prev := uint64(0)
	for range 2 * perTopic {
		id := recv(t, sub).ID
		require.Greater(t, id, prev, "IDs must arrive strictly increasing across topics")
		prev = id
	}
}

func TestPublishSubscribe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("delivers to subscriber", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"orders"})
		require.NoError(t, err)
		defer sub.Close()

		require.NoError(t, hub.Publish(ctx, "orders", []byte("one")))
		msg := recv(t, sub)
		assert.Equal(t, "orders", msg.Topic)
		assert.Equal(t, []byte("one"), msg.Payload)
		assert.Positive(t, msg.ID)
	})

	t.Run("fans out to all subscribers", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		subs := make([]*fanout.Subscription, 3)
		for i := range subs {
			s, err := hub.Subscribe(ctx, []string{"orders"})
			require.NoError(t, err)
			defer s.Close()
			subs[i] = s
		}
		require.NoError(t, hub.Publish(ctx, "orders", []byte("x")))
		for _, s := range subs {
			assert.Equal(t, []byte("x"), recv(t, s).Payload)
		}
	})

	t.Run("topic isolation", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		a, err := hub.Subscribe(ctx, []string{"a"})
		require.NoError(t, err)
		defer a.Close()
		require.NoError(t, hub.Publish(ctx, "b", []byte("x")))
		require.NoError(t, hub.Publish(ctx, "a", []byte("y")))
		assert.Equal(t, []byte("y"), recv(t, a).Payload)
	})

	t.Run("multi-topic subscription with duplicates collapsed", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"a", "b", "a"})
		require.NoError(t, err)
		defer sub.Close()

		require.NoError(t, hub.Publish(ctx, "a", []byte("1")))
		require.NoError(t, hub.Publish(ctx, "b", []byte("2")))
		first, second := recv(t, sub), recv(t, sub)
		assert.Equal(t, "a", first.Topic)
		assert.Equal(t, "b", second.Topic)
		select {
		case m := <-sub.C():
			t.Fatalf("duplicate delivery: %+v", m)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("no subscribers is not an error", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()
		require.NoError(t, hub.Publish(ctx, "nobody", []byte("x")))
	})

	t.Run("ordering preserved per subscriber", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithDefaultBuffer(128))
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"seq"})
		require.NoError(t, err)
		defer sub.Close()

		for i := range 100 {
			require.NoError(t, hub.Publish(ctx, "seq", []byte{byte(i)}))
		}
		var lastID uint64
		for i := range 100 {
			m := recv(t, sub)
			assert.Equal(t, byte(i), m.Payload[0])
			assert.Greater(t, m.ID, lastID)
			lastID = m.ID
		}
	})
}

func TestValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("bad options", func(t *testing.T) {
		t.Parallel()
		for _, opt := range []fanout.Option{
			fanout.WithDefaultBuffer(0),
			fanout.WithReplay(-1),
			fanout.WithReplayTTL(0),
			fanout.WithBus(nil),
			fanout.WithScope(nil),
			fanout.WithClock(nil),
			fanout.WithDefaultPolicy(fanout.OverflowPolicy(99)),
		} {
			_, err := fanout.New(opt)
			assert.Error(t, err)
		}
	})

	t.Run("bad topics", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		assert.ErrorIs(t, hub.Publish(ctx, "", nil), fanout.ErrInvalidTopic)
		assert.ErrorIs(t, hub.Publish(ctx, "a\x00b", nil), fanout.ErrInvalidTopic)
		assert.ErrorIs(t, hub.Publish(ctx, "a\x1fb", nil), fanout.ErrInvalidTopic)

		_, err = hub.Subscribe(ctx, nil)
		assert.ErrorIs(t, err, fanout.ErrNoTopics)
		_, err = hub.Subscribe(ctx, []string{""})
		assert.ErrorIs(t, err, fanout.ErrInvalidTopic)
	})

	t.Run("bad subscribe options", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		_, err = hub.Subscribe(ctx, []string{"t"}, fanout.WithBuffer(0))
		assert.Error(t, err)
		_, err = hub.Subscribe(ctx, []string{"t"}, fanout.WithPolicy(fanout.OverflowPolicy(99)))
		assert.Error(t, err)
	})
}

func TestOverflowPolicies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("drop oldest keeps newest window", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"t"}, fanout.WithBuffer(2))
		require.NoError(t, err)
		defer sub.Close()

		for i := range 4 {
			require.NoError(t, hub.Publish(ctx, "t", []byte{byte(i)}))
		}
		assert.Equal(t, byte(2), recv(t, sub).Payload[0])
		assert.Equal(t, byte(3), recv(t, sub).Payload[0])
		assert.Equal(t, uint64(2), sub.Dropped())
	})

	t.Run("drop newest keeps oldest window", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithDefaultPolicy(fanout.DropNewest))
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"t"}, fanout.WithBuffer(2))
		require.NoError(t, err)
		defer sub.Close()

		for i := range 4 {
			require.NoError(t, hub.Publish(ctx, "t", []byte{byte(i)}))
		}
		assert.Equal(t, byte(0), recv(t, sub).Payload[0])
		assert.Equal(t, byte(1), recv(t, sub).Payload[0])
		assert.Equal(t, uint64(2), sub.Dropped())
	})

	t.Run("close slow tears down", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"t", "u"}, fanout.WithBuffer(1), fanout.WithPolicy(fanout.CloseSlow))
		require.NoError(t, err)
		defer sub.Close()

		require.NoError(t, hub.Publish(ctx, "t", []byte("1")))
		require.NoError(t, hub.Publish(ctx, "t", []byte("2")))
		assert.Equal(t, []byte("1"), recv(t, sub).Payload)
		expectClosed(t, sub)
		assert.ErrorIs(t, sub.Err(), fanout.ErrSlowConsumer)

		// Publishes to any of its former topics keep working.
		require.NoError(t, hub.Publish(ctx, "t", []byte("3")))
		require.NoError(t, hub.Publish(ctx, "u", []byte("4")))
	})

	t.Run("per-subscription policy overrides hub default", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithDefaultPolicy(fanout.CloseSlow))
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"t"}, fanout.WithBuffer(1), fanout.WithPolicy(fanout.DropOldest))
		require.NoError(t, err)
		defer sub.Close()

		require.NoError(t, hub.Publish(ctx, "t", []byte("1")))
		require.NoError(t, hub.Publish(ctx, "t", []byte("2")))
		assert.Equal(t, []byte("2"), recv(t, sub).Payload)
		assert.NoError(t, sub.Err())
	})
}

func TestSubscriptionClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("close is idempotent and err stays nil", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		sub, err := hub.Subscribe(ctx, []string{"t"})
		require.NoError(t, err)
		sub.Close()
		sub.Close()
		expectClosed(t, sub)
		assert.NoError(t, sub.Err())
		require.NoError(t, hub.Publish(ctx, "t", []byte("after")))
	})

	t.Run("close during concurrent publishes", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		var wg sync.WaitGroup
		stop := make(chan struct{})
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_ = hub.Publish(ctx, "t", []byte("x"))
				}
			}
		})
		for range 50 {
			sub, err := hub.Subscribe(ctx, []string{"t"}, fanout.WithBuffer(1))
			require.NoError(t, err)
			sub.Close()
		}
		close(stop)
		wg.Wait()
	})
}

func TestHubClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	hub, err := fanout.New()
	require.NoError(t, err)

	sub, err := hub.Subscribe(ctx, []string{"t"})
	require.NoError(t, err)

	hub.Close()
	hub.Close()

	expectClosed(t, sub)
	assert.ErrorIs(t, sub.Err(), fanout.ErrClosed)
	assert.ErrorIs(t, hub.Publish(ctx, "t", []byte("x")), fanout.ErrClosed)
	_, err = hub.Subscribe(ctx, []string{"t"})
	assert.ErrorIs(t, err, fanout.ErrClosed)

	// Closing an already-torn-down subscription is a no-op.
	sub.Close()
}

// TestCloseSlowDuringSubscribe hammers the window between a subscription
// becoming visible to publishers and Subscribe returning: a CloseSlow
// teardown fired by a publisher in that window must see fully-initialized
// subscription state (caught by -race before the states-assignment order was
// fixed).
func TestCloseSlowDuringSubscribe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	hub, err := fanout.New()
	require.NoError(t, err)
	defer hub.Close()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_ = hub.Publish(ctx, "hot", []byte("x"))
				}
			}
		})
	}
	for range 500 {
		sub, err := hub.Subscribe(ctx, []string{"hot", "cold"}, fanout.WithBuffer(1), fanout.WithPolicy(fanout.CloseSlow))
		require.NoError(t, err)
		expectClosed(t, sub)
		sub.Close()
	}
	close(stop)
	wg.Wait()
}

func TestConcurrencyStress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	hub, err := fanout.New(fanout.WithReplay(8))
	require.NoError(t, err)
	defer hub.Close()

	topics := []string{"a", "b", "c"}
	var wg sync.WaitGroup
	for _, topic := range topics {
		wg.Go(func() {
			for range 500 {
				_ = hub.Publish(ctx, topic, []byte("x"))
			}
		})
	}
	for range 8 {
		wg.Go(func() {
			for range 50 {
				sub, err := hub.Subscribe(ctx, topics, fanout.WithBuffer(4))
				if err != nil {
					if errors.Is(err, fanout.ErrClosed) {
						return
					}
					t.Error(err)
					return
				}
				select {
				case <-sub.C():
				case <-time.After(10 * time.Millisecond):
				}
				sub.Close()
			}
		})
	}
	wg.Wait()
}
