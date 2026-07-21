//go:build integration

package redisbus_test

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/realtime/fanout"
	"github.com/dmitrymomot/forge/realtime/fanout/redisbus"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

var channelSeq atomic.Int64

// uniqueChannel isolates each test on its own Pub/Sub channel so parallel
// tests sharing the container never cross-deliver.
func uniqueChannel(prefix string) string {
	return fmt.Sprintf("%s:%d:%d", prefix, time.Now().UnixNano(), channelSeq.Add(1))
}

func openClient(t *testing.T) goredis.UniversalClient {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: redistest.Addr(t)})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// collector accumulates deliveries for assertion.
type collector struct {
	mu   sync.Mutex
	msgs []fanout.Message
}

func (c *collector) deliver(topic string, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, fanout.Message{Topic: topic, Payload: payload})
}

// find returns the first delivery on topic, if any.
func (c *collector) find(topic string) (fanout.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.msgs {
		if m.Topic == topic {
			return m, true
		}
	}
	return fanout.Message{}, false
}

// runBus starts the receive loop and returns once publishing round-trips,
// proving the subscription is active. The probe handler is left registered;
// register the real consumer (or attach the hub) after runBus returns.
func runBus(t *testing.T, bus *redisbus.Bus) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = bus.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("bus did not stop")
		}
	})
	probe := &collector{}
	bus.Subscribe(probe.deliver)
	require.Eventually(t, func() bool {
		if err := bus.Publish(ctx, "probe", nil); err != nil {
			return false
		}
		_, ok := probe.find("probe")
		return ok
	}, 10*time.Second, 50*time.Millisecond, "subscription never became active")
}

func TestPublishReceive_Integration(t *testing.T) {
	t.Parallel()
	client := openClient(t)
	channel := uniqueChannel("fanout:pr")

	sender, err := redisbus.New(client, redisbus.WithChannel(channel))
	require.NoError(t, err)
	receiver, err := redisbus.New(client, redisbus.WithChannel(channel))
	require.NoError(t, err)

	runBus(t, sender)
	runBus(t, receiver)

	got := &collector{}
	receiver.Subscribe(got.deliver)
	self := &collector{}
	sender.Subscribe(self.deliver)

	payload := []byte{0x00, 0x1f, 0xff, 'b', 'i', 'n'}
	require.NoError(t, sender.Publish(context.Background(), "orders.42", payload))

	require.Eventually(t, func() bool {
		_, ok := got.find("orders.42")
		return ok
	}, 10*time.Second, 50*time.Millisecond)
	msg, _ := got.find("orders.42")
	assert.True(t, bytes.Equal(payload, msg.Payload), "binary payload must survive the frame")

	require.Eventually(t, func() bool {
		_, ok := self.find("orders.42")
		return ok
	}, 10*time.Second, 50*time.Millisecond, "the publishing instance must receive its own message")
}

func TestChannelIsolation_Integration(t *testing.T) {
	t.Parallel()
	client := openClient(t)

	busA, err := redisbus.New(client, redisbus.WithChannel(uniqueChannel("fanout:iso:a")))
	require.NoError(t, err)
	busB, err := redisbus.New(client, redisbus.WithChannel(uniqueChannel("fanout:iso:b")))
	require.NoError(t, err)
	runBus(t, busA)
	runBus(t, busB)

	got := &collector{}
	busB.Subscribe(got.deliver)
	require.NoError(t, busA.Publish(context.Background(), "crossing", []byte("x")))
	time.Sleep(500 * time.Millisecond)
	_, ok := got.find("crossing")
	assert.False(t, ok, "messages must not cross channels")
}

func TestHubEndToEnd_Integration(t *testing.T) {
	t.Parallel()
	client := openClient(t)
	channel := uniqueChannel("fanout:e2e")

	bus1, err := redisbus.New(client, redisbus.WithChannel(channel))
	require.NoError(t, err)
	bus2, err := redisbus.New(client, redisbus.WithChannel(channel))
	require.NoError(t, err)
	runBus(t, bus1)
	runBus(t, bus2)

	// Attaching the hubs after runBus replaces the probe handlers with the
	// hubs' dispatch.
	hub1, err := fanout.New(fanout.WithBus(bus1))
	require.NoError(t, err)
	defer hub1.Close()
	hub2, err := fanout.New(fanout.WithBus(bus2))
	require.NoError(t, err)
	defer hub2.Close()

	local, err := hub1.Subscribe(context.Background(), []string{"chat"})
	require.NoError(t, err)
	defer local.Close()
	remote, err := hub2.Subscribe(context.Background(), []string{"chat"})
	require.NoError(t, err)
	defer remote.Close()

	require.NoError(t, hub1.Publish(context.Background(), "chat", []byte("hello")))

	for name, sub := range map[string]*fanout.Subscription{"local": local, "remote": remote} {
		select {
		case msg := <-sub.C():
			assert.Equal(t, "chat", msg.Topic, name)
			assert.Equal(t, []byte("hello"), msg.Payload, name)
		case <-time.After(10 * time.Second):
			t.Fatalf("%s subscriber never received the message", name)
		}
	}
}
