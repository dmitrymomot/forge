package eventrouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/eventrouter"
	"github.com/dmitrymomot/forge/async/queue"
)

type payload struct {
	V string `json:"v"`
}

// stub is a scripted Deliverer: fn (when set) decides each call's outcome by
// call ordinal; calls are recorded as defensive copies.
type stub struct {
	fn    func(ctx context.Context, call int, events []eventrouter.Event) error
	calls [][]eventrouter.Event
	mu    sync.Mutex
}

func (s *stub) Deliver(ctx context.Context, events []eventrouter.Event) error {
	s.mu.Lock()
	call := len(s.calls)
	s.calls = append(s.calls, slices.Clone(events))
	fn := s.fn
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, call, events)
	}
	return nil
}

func (s *stub) snapshot() [][]eventrouter.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]eventrouter.Event, len(s.calls))
	copy(out, s.calls)
	return out
}

// publishAll publishes one event per payload value concurrently on the sync
// bus and returns each publish's error keyed by value.
func publishAll(ctx context.Context, bus *eventbus.Bus, evt eventbus.Event[payload], values ...string) map[string]error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := make(map[string]error, len(values))
	for _, v := range values {
		wg.Go(func() {
			err := eventbus.Publish(ctx, bus, evt, payload{V: v})
			mu.Lock()
			errs[v] = err
			mu.Unlock()
		})
	}
	wg.Wait()
	return errs
}

func TestDestination_BatchBySize(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("size", s,
		eventrouter.WithBatchSize(3), eventrouter.WithBatchAge(time.Hour))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.size")
	eventrouter.Route(bus, evt, dest)

	errs := publishAll(context.Background(), bus, evt, "a", "b", "c")
	for v, err := range errs {
		assert.NoError(t, err, "publish %q", v)
	}
	calls := s.snapshot()
	require.Len(t, calls, 1, "size-filled batch flushes as one delivery")
	require.Len(t, calls[0], 3)

	ids := make(map[string]struct{})
	for _, e := range calls[0] {
		assert.Equal(t, "router.size", e.Name)
		assert.NotEmpty(t, e.ID)
		assert.False(t, e.OccurredAt.IsZero())
		ids[e.ID] = struct{}{}
	}
	assert.Len(t, ids, 3, "every event keeps its own stable id")
}

func TestDestination_BatchByAge(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("age", s,
		eventrouter.WithBatchSize(100), eventrouter.WithBatchAge(150*time.Millisecond))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.age")
	eventrouter.Route(bus, evt, dest)

	errs := publishAll(context.Background(), bus, evt, "a", "b")
	for v, err := range errs {
		assert.NoError(t, err, "publish %q", v)
	}
	calls := s.snapshot()
	require.Len(t, calls, 1, "partial batch flushes by age")
	assert.Len(t, calls[0], 2)
}

func TestDestination_TransientErrorFailsWholeBatch(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	s := &stub{fn: func(context.Context, int, []eventrouter.Event) error { return errBoom }}
	dest := eventrouter.NewDestination("transient", s,
		eventrouter.WithBatchSize(2), eventrouter.WithBatchAge(time.Hour))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.transient")
	eventrouter.Route(bus, evt, dest)

	errs := publishAll(context.Background(), bus, evt, "a", "b")
	for v, err := range errs {
		require.Error(t, err, "publish %q", v)
		assert.ErrorIs(t, err, errBoom)
		assert.False(t, queue.IsSkipRetry(err), "transient failures retry, never dead-letter")
	}
}

func TestDestination_PermanentSingletonDeadLetters(t *testing.T) {
	t.Parallel()
	errBad := errors.New("rejected")
	s := &stub{fn: func(context.Context, int, []eventrouter.Event) error {
		return eventrouter.Permanent(errBad)
	}}
	dest := eventrouter.NewDestination("permanent", s, eventrouter.WithBatchSize(1))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.permanent")
	eventrouter.Route(bus, evt, dest)

	err := eventbus.Publish(context.Background(), bus, evt, payload{V: "a"})
	require.Error(t, err)
	assert.True(t, queue.IsSkipRetry(err), "permanent failures dead-letter without burning attempts")
	assert.ErrorIs(t, err, eventrouter.ErrPermanent)
	assert.ErrorIs(t, err, errBad)
}

func TestDestination_PoisonIsolation(t *testing.T) {
	t.Parallel()
	poison := `{"v":"poison"}`
	s := &stub{fn: func(_ context.Context, _ int, events []eventrouter.Event) error {
		for _, e := range events {
			if string(e.Payload) == poison {
				return eventrouter.Permanent(errors.New("bad event"))
			}
		}
		return nil
	}}
	dest := eventrouter.NewDestination("isolate", s,
		eventrouter.WithBatchSize(3), eventrouter.WithBatchAge(time.Hour))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.isolate")
	eventrouter.Route(bus, evt, dest)

	errs := publishAll(context.Background(), bus, evt, "a", "poison", "b")
	assert.NoError(t, errs["a"], "innocent batchmates are acknowledged")
	assert.NoError(t, errs["b"], "innocent batchmates are acknowledged")
	require.Error(t, errs["poison"])
	assert.True(t, queue.IsSkipRetry(errs["poison"]), "the poison event dead-letters alone")
	assert.ErrorIs(t, errs["poison"], eventrouter.ErrPermanent)

	calls := s.snapshot()
	require.Len(t, calls, 4, "one batch attempt plus one isolation delivery per event")
	assert.Len(t, calls[0], 3)
	for _, call := range calls[1:] {
		assert.Len(t, call, 1)
	}
}

func TestDestination_IsolationVerdictsSplit(t *testing.T) {
	t.Parallel()
	errTransient := errors.New("flaky")
	s := &stub{fn: func(_ context.Context, call int, events []eventrouter.Event) error {
		if call == 0 {
			return eventrouter.Permanent(errors.New("batch rejected"))
		}
		switch string(events[0].Payload) {
		case `{"v":"poison"}`:
			return eventrouter.Permanent(errors.New("bad event"))
		case `{"v":"flaky"}`:
			return errTransient
		default:
			return nil
		}
	}}
	dest := eventrouter.NewDestination("split", s,
		eventrouter.WithBatchSize(3), eventrouter.WithBatchAge(time.Hour))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.split")
	eventrouter.Route(bus, evt, dest)

	errs := publishAll(context.Background(), bus, evt, "ok", "poison", "flaky")
	assert.NoError(t, errs["ok"])
	assert.True(t, queue.IsSkipRetry(errs["poison"]))
	require.Error(t, errs["flaky"])
	assert.ErrorIs(t, errs["flaky"], errTransient)
	assert.False(t, queue.IsSkipRetry(errs["flaky"]), "transient isolation outcomes keep retrying")
}

func TestDestination_DelivererPanicIsRetryable(t *testing.T) {
	t.Parallel()
	s := &stub{fn: func(context.Context, int, []eventrouter.Event) error { panic("kaboom") }}
	dest := eventrouter.NewDestination("panicky", s, eventrouter.WithBatchSize(1))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.panic")
	eventrouter.Route(bus, evt, dest)

	err := eventbus.Publish(context.Background(), bus, evt, payload{V: "a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deliverer panic")
	assert.False(t, queue.IsSkipRetry(err))
}

func TestDestination_DeliveryTimeout(t *testing.T) {
	t.Parallel()
	s := &stub{fn: func(ctx context.Context, _ int, _ []eventrouter.Event) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	dest := eventrouter.NewDestination("slow", s,
		eventrouter.WithBatchSize(1), eventrouter.WithDeliveryTimeout(30*time.Millisecond))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.slow")
	eventrouter.Route(bus, evt, dest)

	err := eventbus.Publish(context.Background(), bus, evt, payload{V: "a"})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

type scopeKey struct{}

func scopeHook(ctx context.Context) (string, error) {
	s, _ := ctx.Value(scopeKey{}).(string)
	return s, nil
}

func TestDestination_ScopeKeysBatches(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("tenant", s,
		eventrouter.WithBatchSize(2), eventrouter.WithBatchAge(150*time.Millisecond),
		eventrouter.WithScope(scopeHook))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.tenant")
	eventrouter.Route(bus, evt, dest)

	ctxA := context.WithValue(context.Background(), scopeKey{}, "tenant-a")
	ctxB := context.WithValue(context.Background(), scopeKey{}, "tenant-b")

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i, c := range []struct {
		ctx context.Context
		v   string
	}{{ctxA, "a1"}, {ctxA, "a2"}, {ctxB, "b1"}} {
		wg.Go(func() {
			errs[i] = eventbus.Publish(c.ctx, bus, evt, payload{V: c.v})
		})
	}
	wg.Wait()
	for i, err := range errs {
		assert.NoError(t, err, "publish %d", i)
	}

	calls := s.snapshot()
	require.Len(t, calls, 2, "one batch per tenant")
	for _, call := range calls {
		tenants := make(map[string]struct{})
		for _, e := range call {
			var p payload
			require.NoError(t, json.Unmarshal(e.Payload, &p))
			tenants[p.V[:1]] = struct{}{}
		}
		assert.Len(t, tenants, 1, "batches never mix tenants")
	}
}

func TestDestination_ScopeFailsClosed(t *testing.T) {
	t.Parallel()
	s := &stub{}
	dest := eventrouter.NewDestination("closed", s,
		eventrouter.WithBatchSize(1), eventrouter.WithScope(scopeHook))
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("router.closed")
	eventrouter.Route(bus, evt, dest)

	err := eventbus.Publish(context.Background(), bus, evt, payload{V: "a"})
	require.Error(t, err)
	assert.ErrorIs(t, err, eventrouter.ErrScopeMissing)
	assert.Empty(t, s.snapshot(), "nothing is delivered without a scope")

	hookErr := errors.New("no tenant")
	evt2 := eventbus.NewEvent[payload]("router.closed2")
	failing := eventrouter.NewDestination("closed2", s,
		eventrouter.WithBatchSize(1),
		eventrouter.WithScope(func(context.Context) (string, error) { return "", hookErr }))
	eventrouter.Route(bus, evt2, failing)
	err = eventbus.Publish(context.Background(), bus, evt2, payload{V: "a"})
	require.Error(t, err)
	assert.ErrorIs(t, err, eventrouter.ErrScopeMissing)
	assert.ErrorIs(t, err, hookErr)
}

func TestNewDestination_Validation(t *testing.T) {
	t.Parallel()
	s := &stub{}
	assert.Panics(t, func() { eventrouter.NewDestination("", s) })
	assert.Panics(t, func() { eventrouter.NewDestination("d", nil) })
	assert.Panics(t, func() { eventrouter.WithBatchSize(0) })
	assert.Panics(t, func() { eventrouter.WithBatchAge(0) })
	assert.Panics(t, func() { eventrouter.WithDeliveryTimeout(-time.Second) })
	assert.Panics(t, func() { eventrouter.WithScope(nil) })
	assert.Equal(t, "d", eventrouter.NewDestination("d", s).Name())
}

func TestPermanent_NilIsNil(t *testing.T) {
	t.Parallel()
	assert.NoError(t, eventrouter.Permanent(nil))
}
