package eventrouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/eventrouter"
	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

func TestRoute_PayloadPassthrough(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("raw", s, eventrouter.WithBatchSize(1))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("route.raw")
	eventrouter.Route(bus, evt, dest)

	require.NoError(t, eventbus.Publish(context.Background(), bus, evt, payload{V: "hello"}))
	calls := s.snapshot()
	require.Len(t, calls, 1)
	assert.JSONEq(t, `{"v":"hello"}`, string(calls[0][0].Payload))
}

func TestRoute_Filter(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("filtered", s, eventrouter.WithBatchSize(1))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("route.filtered")
	eventrouter.Route(bus, evt, dest, eventrouter.WithFilter(func(d eventbus.Delivery[payload]) bool {
		return d.Payload.V != "skip"
	}))

	require.NoError(t, eventbus.Publish(context.Background(), bus, evt, payload{V: "skip"}))
	assert.Empty(t, s.snapshot(), "filtered events are acknowledged without delivery")

	require.NoError(t, eventbus.Publish(context.Background(), bus, evt, payload{V: "keep"}))
	require.Len(t, s.snapshot(), 1)
}

func TestRoute_Remap(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("remapped", s, eventrouter.WithBatchSize(1))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("route.remapped")
	eventrouter.Route(bus, evt, dest, eventrouter.WithRemap(func(d eventbus.Delivery[payload]) (any, error) {
		return map[string]string{"value": d.Payload.V, "event": d.Name}, nil
	}))

	require.NoError(t, eventbus.Publish(context.Background(), bus, evt, payload{V: "x"}))
	calls := s.snapshot()
	require.Len(t, calls, 1)
	assert.JSONEq(t, `{"value":"x","event":"route.remapped"}`, string(calls[0][0].Payload))
}

func TestRoute_RemapErrorIsPoison(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("poisonremap", s, eventrouter.WithBatchSize(1))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("route.poisonremap")
	errMap := errors.New("cannot map")
	eventrouter.Route(bus, evt, dest, eventrouter.WithRemap(func(eventbus.Delivery[payload]) (any, error) {
		return nil, errMap
	}))

	err := eventbus.Publish(context.Background(), bus, evt, payload{V: "x"})
	require.Error(t, err)
	assert.True(t, queue.IsSkipRetry(err), "a deterministic remap failure dead-letters")
	assert.ErrorIs(t, err, errMap)
	assert.Empty(t, s.snapshot())
}

func TestRoute_UnmarshalableRemapIsPoison(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("badjson", s, eventrouter.WithBatchSize(1))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("route.badjson")
	eventrouter.Route(bus, evt, dest, eventrouter.WithRemap(func(eventbus.Delivery[payload]) (any, error) {
		return make(chan int), nil
	}))

	err := eventbus.Publish(context.Background(), bus, evt, payload{V: "x"})
	require.Error(t, err)
	assert.True(t, queue.IsSkipRetry(err))
	assert.Empty(t, s.snapshot())
}

func TestRoute_Validation(t *testing.T) {
	t.Parallel()
	s := &stub{}
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("route.validation")
	assert.Panics(t, func() { eventrouter.Route(bus, evt, nil) })
	assert.Panics(t, func() { eventrouter.WithFilter[payload](nil) })
	assert.Panics(t, func() { eventrouter.WithRemap[payload](nil) })

	dest := eventrouter.NewDestination("dup", s)
	eventrouter.Route(bus, evt, dest)
	assert.Panics(t, func() { eventrouter.Route(bus, evt, dest) }, "duplicate (event, destination) pair")
}

func TestRoute_DurableEndToEnd(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("warehouse", s,
		eventrouter.WithBatchSize(2), eventrouter.WithBatchAge(300*time.Millisecond))
	broker := queue.NewMemoryBroker()
	bus := eventbus.New(broker)
	evt := eventbus.NewEvent[payload]("durable.order")
	eventrouter.Route(bus, evt, dest)

	svc, err := eventbus.NewService(bus, queue.WithConfig(fastQueueConfig()))
	require.NoError(t, err)
	stop := startService(t, svc)
	defer stop()

	require.NoError(t, eventbus.Publish(context.Background(), bus, evt, payload{V: "a"}))
	require.NoError(t, eventbus.Publish(context.Background(), bus, evt, payload{V: "b"}))

	require.Eventually(t, func() bool {
		total := 0
		for _, call := range s.snapshot() {
			total += len(call)
		}
		return total == 2
	}, 5*time.Second, 10*time.Millisecond, "both events are delivered")

	ids := make(map[string]struct{})
	for _, call := range s.snapshot() {
		for _, e := range call {
			assert.Equal(t, "durable.order", e.Name)
			ids[e.ID] = struct{}{}
		}
	}
	assert.Len(t, ids, 2)
}

func TestRoute_DurableRetryKeepsEventID(t *testing.T) {
	t.Parallel()
	errFlaky := errors.New("first attempt fails")
	s := &stub{fn: func(_ context.Context, call int, _ []eventrouter.Event) error {
		if call == 0 {
			return errFlaky
		}
		return nil
	}}
	dest := eventrouter.NewDestination("flaky", s, eventrouter.WithBatchSize(1))
	broker := queue.NewMemoryBroker()
	bus := eventbus.New(broker)
	evt := eventbus.NewEvent[payload]("durable.flaky")
	eventrouter.Route(bus, evt, dest, eventrouter.WithSubscribeOptions[payload](
		eventbus.WithRetryBackoff(backoff.Constant(5*time.Millisecond)),
		eventbus.WithMaxAttempts(3),
	))

	svc, err := eventbus.NewService(bus, queue.WithConfig(fastQueueConfig()))
	require.NoError(t, err)
	stop := startService(t, svc)
	defer stop()

	require.NoError(t, eventbus.Publish(context.Background(), bus, evt, payload{V: "a"}))

	require.Eventually(t, func() bool { return len(s.snapshot()) == 2 },
		5*time.Second, 10*time.Millisecond, "the failed delivery retries")
	calls := s.snapshot()
	assert.Equal(t, calls[0][0].ID, calls[1][0].ID, "retries carry the same stable event id")

	var p payload
	require.NoError(t, json.Unmarshal(calls[1][0].Payload, &p))
	assert.Equal(t, "a", p.V)
}
