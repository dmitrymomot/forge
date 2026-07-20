package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/outbox"
	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// flakyBroker fails Push per-job via failJob; everything else delegates.
type flakyBroker struct {
	queue.Broker
	failJob func(j queue.Job) error
	pushes  int
	mu      sync.Mutex
}

func (f *flakyBroker) Push(ctx context.Context, jobs ...queue.Job) error {
	f.mu.Lock()
	f.pushes++
	fail := f.failJob
	f.mu.Unlock()
	if fail != nil {
		for _, j := range jobs {
			if err := fail(j); err != nil {
				return err
			}
		}
	}
	return f.Broker.Push(ctx, jobs...)
}

func (f *flakyBroker) setFail(fn func(j queue.Job) error) {
	f.mu.Lock()
	f.failJob = fn
	f.mu.Unlock()
}

func (f *flakyBroker) pushCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pushes
}

func fastConfig() outbox.Config {
	return outbox.Config{BatchSize: 2, PollInterval: 5 * time.Millisecond, Lease: time.Minute}
}

func runRelay(t *testing.T, r *outbox.Relay) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func brokerPending(t *testing.T, b queue.Broker, q string) int {
	t.Helper()
	st, err := b.Stats(context.Background())
	require.NoError(t, err)
	return st[q].Pending
}

func storePending(t *testing.T, s outbox.Store) int {
	t.Helper()
	st, err := s.Stats(context.Background())
	require.NoError(t, err)
	return st.Pending
}

func TestNewRelay_Validation(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	broker := queue.NewMemoryBroker()

	_, err := outbox.NewRelay(nil, broker)
	assert.ErrorIs(t, err, outbox.ErrInvalidConfig)

	_, err = outbox.NewRelay(store, nil)
	assert.ErrorIs(t, err, outbox.ErrInvalidConfig)

	_, err = outbox.NewRelay(store, broker, outbox.WithConfig(outbox.Config{}))
	assert.ErrorIs(t, err, outbox.ErrInvalidConfig)
}

func TestRelay_Name(t *testing.T) {
	t.Parallel()
	r, err := outbox.NewRelay(outbox.NewMemoryStore(), queue.NewMemoryBroker())
	require.NoError(t, err)
	assert.Equal(t, "outbox", r.Name())

	r, err = outbox.NewRelay(outbox.NewMemoryStore(), queue.NewMemoryBroker(), outbox.WithName("outbox.billing"))
	require.NoError(t, err)
	assert.Equal(t, "outbox.billing", r.Name())
}

func TestRelay_ForwardsRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := outbox.NewMemoryStore()
	broker := queue.NewMemoryBroker()

	// 5 rows over batch size 2 also exercises the full-batch drain loop.
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		require.NoError(t, store.Add(ctx, nil, makeJob(id, testEpoch)))
	}

	r, err := outbox.NewRelay(store, broker, outbox.WithConfig(fastConfig()))
	require.NoError(t, err)
	runRelay(t, r)

	require.Eventually(t, func() bool {
		return brokerPending(t, broker, "default") == 5 && storePending(t, store) == 0
	}, 3*time.Second, 5*time.Millisecond, "all rows forwarded and deleted")
}

func TestRelay_BrokerDownThenRecovers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := outbox.NewMemoryStore()
	down := errors.New("broker down")
	broker := &flakyBroker{Broker: queue.NewMemoryBroker()}
	broker.setFail(func(queue.Job) error { return down })

	require.NoError(t, store.Add(ctx, nil, makeJob("a", testEpoch)))

	r, err := outbox.NewRelay(store, broker, outbox.WithConfig(fastConfig()),
		outbox.WithBackoff(backoff.Constant(time.Millisecond)))
	require.NoError(t, err)
	runRelay(t, r)

	require.Eventually(t, func() bool { return broker.pushCalls() >= 3 },
		3*time.Second, 5*time.Millisecond, "row is retried while the broker is down")
	assert.Equal(t, 1, storePending(t, store), "row is never dropped")

	broker.setFail(nil)
	require.Eventually(t, func() bool {
		return brokerPending(t, broker, "default") == 1 && storePending(t, store) == 0
	}, 3*time.Second, 5*time.Millisecond, "backlog drains once the broker recovers")
}

func TestRelay_PoisonRowIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := outbox.NewMemoryStore()
	broker := &flakyBroker{Broker: queue.NewMemoryBroker()}
	broker.setFail(func(j queue.Job) error {
		if j.ID == "poison" {
			return errors.New("payload rejected")
		}
		return nil
	})

	require.NoError(t, store.Add(ctx, nil,
		makeJob("a", testEpoch),
		makeJob("poison", testEpoch.Add(time.Second)),
		makeJob("b", testEpoch.Add(2*time.Second)),
	))

	cfg := fastConfig()
	cfg.BatchSize = 3
	// Default backoff (5s base) keeps the poison row out of re-claim range for
	// the duration of the test, so the healthy-row assertion is stable.
	r, err := outbox.NewRelay(store, broker, outbox.WithConfig(cfg))
	require.NoError(t, err)
	runRelay(t, r)

	require.Eventually(t, func() bool {
		return brokerPending(t, broker, "default") == 2
	}, 3*time.Second, 5*time.Millisecond, "healthy rows pass despite the poison batch member")
	assert.Equal(t, 1, storePending(t, store), "poison row stays parked in backoff")
}

func TestRelay_BackoffUsesInjectedClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewMock(testEpoch)
	store := outbox.NewMemoryStore(outbox.WithMemoryClock(clk))
	broker := &flakyBroker{Broker: queue.NewMemoryBroker()}
	broker.setFail(func(queue.Job) error { return errors.New("down") })

	require.NoError(t, store.Add(ctx, nil, makeJob("a", testEpoch)))

	// retryAt = mock now + 10s: with the mock frozen, the row must stay in
	// backoff no matter how much wall time the fast poll loop burns.
	r, err := outbox.NewRelay(store, broker, outbox.WithConfig(fastConfig()),
		outbox.WithClock(clk), outbox.WithBackoff(backoff.Constant(10*time.Second)))
	require.NoError(t, err)
	runRelay(t, r)

	// One failed cycle costs two Push calls (batch, then per-row fallback).
	require.Eventually(t, func() bool { return broker.pushCalls() >= 1 },
		3*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond) // let the in-flight cycle finish; row is now parked
	settled := broker.pushCalls()
	time.Sleep(50 * time.Millisecond) // many poll cycles at 5ms
	assert.Equal(t, settled, broker.pushCalls(), "row stays in backoff until the injected clock advances")

	clk.Advance(10 * time.Second)
	require.Eventually(t, func() bool { return broker.pushCalls() > settled },
		3*time.Second, 5*time.Millisecond, "row is due again once the clock passes retryAt")
}

func TestRelay_WithLogger(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	store := outbox.NewMemoryStore()
	require.NoError(t, store.Add(context.Background(), nil, makeJob("a", testEpoch)))
	broker := queue.NewMemoryBroker()
	r, err := outbox.NewRelay(store, broker, outbox.WithConfig(fastConfig()), outbox.WithLogger(log))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	require.Eventually(t, func() bool { return brokerPending(t, broker, "default") == 1 },
		3*time.Second, 5*time.Millisecond)
	cancel()
	<-done // stop the relay before reading buf: strings.Builder is not synchronized

	assert.Contains(t, buf.String(), "outbox relay started")
	assert.Contains(t, buf.String(), "outbox forwarded")
}

func TestRelay_StopsOnCancel(t *testing.T) {
	t.Parallel()
	r, err := outbox.NewRelay(outbox.NewMemoryStore(), queue.NewMemoryBroker(), outbox.WithConfig(fastConfig()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not stop on cancel")
	}
}
