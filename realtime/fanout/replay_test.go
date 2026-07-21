package fanout_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/realtime/fanout"
)

func TestReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("resume after id", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithReplay(4))
		require.NoError(t, err)
		defer hub.Close()

		for i := range 6 {
			require.NoError(t, hub.Publish(ctx, "t", []byte{byte(i)}))
		}
		// The ring holds the newest 4 (payloads 2..5). Grab their IDs via a
		// full resume first.
		all, err := hub.Subscribe(ctx, []string{"t"}, fanout.WithResumeAfter(0))
		require.NoError(t, err)
		defer all.Close()
		first := recv(t, all)
		assert.Equal(t, byte(2), first.Payload[0])

		sub, err := hub.Subscribe(ctx, []string{"t"}, fanout.WithResumeAfter(first.ID))
		require.NoError(t, err)
		defer sub.Close()
		for want := byte(3); want <= 5; want++ {
			assert.Equal(t, want, recv(t, sub).Payload[0])
		}
		// Live delivery continues seamlessly after the replayed backlog.
		require.NoError(t, hub.Publish(ctx, "t", []byte{9}))
		assert.Equal(t, byte(9), recv(t, sub).Payload[0])
	})

	t.Run("resume without replay errors", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New()
		require.NoError(t, err)
		defer hub.Close()

		_, err = hub.Subscribe(ctx, []string{"t"}, fanout.WithResumeAfter(0))
		assert.ErrorIs(t, err, fanout.ErrReplayDisabled)
	})

	t.Run("backlog exceeding buffer keeps newest and counts drops", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithReplay(8))
		require.NoError(t, err)
		defer hub.Close()

		for i := range 6 {
			require.NoError(t, hub.Publish(ctx, "t", []byte{byte(i)}))
		}
		sub, err := hub.Subscribe(ctx, []string{"t"}, fanout.WithResumeAfter(0), fanout.WithBuffer(2))
		require.NoError(t, err)
		defer sub.Close()
		assert.Equal(t, byte(4), recv(t, sub).Payload[0])
		assert.Equal(t, byte(5), recv(t, sub).Payload[0])
		assert.Equal(t, uint64(4), sub.Dropped())
	})

	t.Run("multi-topic resume merges in id order", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithReplay(4))
		require.NoError(t, err)
		defer hub.Close()

		require.NoError(t, hub.Publish(ctx, "a", []byte("1")))
		require.NoError(t, hub.Publish(ctx, "b", []byte("2")))
		require.NoError(t, hub.Publish(ctx, "a", []byte("3")))

		sub, err := hub.Subscribe(ctx, []string{"a", "b"}, fanout.WithResumeAfter(0))
		require.NoError(t, err)
		defer sub.Close()
		assert.Equal(t, []byte("1"), recv(t, sub).Payload)
		assert.Equal(t, []byte("2"), recv(t, sub).Payload)
		assert.Equal(t, []byte("3"), recv(t, sub).Payload)
	})

	t.Run("idle rings swept after ttl", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewMock(time.Now())
		hub, err := fanout.New(fanout.WithReplay(4), fanout.WithReplayTTL(time.Minute), fanout.WithClock(clk))
		require.NoError(t, err)
		defer hub.Close()

		require.NoError(t, hub.Publish(ctx, "old", []byte("x")))
		clk.Advance(2 * time.Minute)
		// A publish on any topic triggers the amortized sweep.
		require.NoError(t, hub.Publish(ctx, "fresh", []byte("y")))

		sub, err := hub.Subscribe(ctx, []string{"old"}, fanout.WithResumeAfter(0))
		require.NoError(t, err)
		defer sub.Close()
		select {
		case m := <-sub.C():
			t.Fatalf("expected swept ring, got %+v", m)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("subscribed topics survive the sweep", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewMock(time.Now())
		hub, err := fanout.New(fanout.WithReplay(4), fanout.WithReplayTTL(time.Minute), fanout.WithClock(clk))
		require.NoError(t, err)
		defer hub.Close()

		keeper, err := hub.Subscribe(ctx, []string{"kept"})
		require.NoError(t, err)
		defer keeper.Close()

		require.NoError(t, hub.Publish(ctx, "kept", []byte("x")))
		clk.Advance(2 * time.Minute)
		require.NoError(t, hub.Publish(ctx, "other", []byte("y")))

		require.NoError(t, hub.Publish(ctx, "kept", []byte("z")))
		assert.Equal(t, []byte("x"), recv(t, keeper).Payload)
		assert.Equal(t, []byte("z"), recv(t, keeper).Payload)
	})
}
