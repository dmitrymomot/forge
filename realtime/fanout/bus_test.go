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

// fakeBus is an in-memory fanout.Bus connecting any number of hubs, standing
// in for pgbus/redisbus.
type fakeBus struct {
	mu       sync.Mutex
	handlers []func(topic string, payload []byte)
	err      error
}

func (b *fakeBus) Publish(_ context.Context, topic string, payload []byte) error {
	b.mu.Lock()
	handlers := append([]func(string, []byte){}, b.handlers...)
	err := b.err
	b.mu.Unlock()
	if err != nil {
		return err
	}
	for _, fn := range handlers {
		fn(topic, payload)
	}
	return nil
}

func (b *fakeBus) Subscribe(fn func(topic string, payload []byte)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, fn)
}

func (b *fakeBus) fail(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
}

func TestBus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("delivery spans hubs including the publisher's own", func(t *testing.T) {
		t.Parallel()
		bus := &fakeBus{}
		hub1, err := fanout.New(fanout.WithBus(bus))
		require.NoError(t, err)
		defer hub1.Close()
		hub2, err := fanout.New(fanout.WithBus(bus))
		require.NoError(t, err)
		defer hub2.Close()

		local, err := hub1.Subscribe(ctx, []string{"t"})
		require.NoError(t, err)
		defer local.Close()
		remote, err := hub2.Subscribe(ctx, []string{"t"})
		require.NoError(t, err)
		defer remote.Close()

		require.NoError(t, hub1.Publish(ctx, "t", []byte("x")))
		assert.Equal(t, []byte("x"), recv(t, local).Payload)
		assert.Equal(t, []byte("x"), recv(t, remote).Payload)
	})

	t.Run("bus publish error propagates", func(t *testing.T) {
		t.Parallel()
		bus := &fakeBus{}
		hub, err := fanout.New(fanout.WithBus(bus))
		require.NoError(t, err)
		defer hub.Close()

		boom := errors.New("bus down")
		bus.fail(boom)
		assert.ErrorIs(t, hub.Publish(ctx, "t", []byte("x")), boom)
	})

	t.Run("scope rides the bus", func(t *testing.T) {
		t.Parallel()
		bus := &fakeBus{}
		hub1, err := fanout.New(fanout.WithBus(bus), fanout.WithScope(tenantScope))
		require.NoError(t, err)
		defer hub1.Close()
		hub2, err := fanout.New(fanout.WithBus(bus), fanout.WithScope(tenantScope))
		require.NoError(t, err)
		defer hub2.Close()

		alpha, err := hub2.Subscribe(tenantCtx("alpha"), []string{"orders"})
		require.NoError(t, err)
		defer alpha.Close()
		beta, err := hub2.Subscribe(tenantCtx("beta"), []string{"orders"})
		require.NoError(t, err)
		defer beta.Close()

		require.NoError(t, hub1.Publish(tenantCtx("alpha"), "orders", []byte("a")))
		msg := recv(t, alpha)
		assert.Equal(t, "orders", msg.Topic)
		select {
		case m := <-beta.C():
			t.Fatalf("cross-tenant delivery over bus: %+v", m)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("closed hub ignores bus deliveries", func(t *testing.T) {
		t.Parallel()
		bus := &fakeBus{}
		hub, err := fanout.New(fanout.WithBus(bus), fanout.WithReplay(4))
		require.NoError(t, err)
		hub.Close()

		// Dispatch directly, as a driver would after the hub shut down.
		for _, fn := range bus.handlers {
			fn("t", []byte("late"))
		}
	})
}
