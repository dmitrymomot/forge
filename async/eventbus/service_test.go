package eventbus_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/queue"
)

func fastConfig() queue.Config {
	cfg := queue.DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	return cfg
}

// runService starts svc and returns a stop func that cancels it and waits for
// drain.
func runService(t *testing.T, svc *queue.Service) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = svc.Run(ctx); close(stopped) }()
	return func() {
		cancel()
		<-stopped
	}
}

func TestNewService(t *testing.T) {
	t.Parallel()
	evt := eventbus.NewEvent[userCreated]("user.created")

	t.Run("sync bus", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		eventbus.Subscribe(bus, evt, "s", func(context.Context, eventbus.Delivery[userCreated]) error { return nil })
		_, err := eventbus.NewService(bus)
		assert.ErrorIs(t, err, eventbus.ErrNotDurable)
	})

	t.Run("no subscriptions", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.New(queue.NewMemoryBroker())
		_, err := eventbus.NewService(bus)
		assert.ErrorIs(t, err, eventbus.ErrNoSubscriptions)
	})

	t.Run("default name is eventbus and caller can override", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.New(queue.NewMemoryBroker())
		eventbus.Subscribe(bus, evt, "s", func(context.Context, eventbus.Delivery[userCreated]) error { return nil })
		svc, err := eventbus.NewService(bus)
		require.NoError(t, err)
		assert.Equal(t, "eventbus", svc.Name())

		named, err := eventbus.NewService(bus, queue.WithName("events-worker"))
		require.NoError(t, err)
		assert.Equal(t, "events-worker", named.Name())
	})
}

func TestService_EndToEnd(t *testing.T) {
	t.Parallel()
	evt := eventbus.NewEvent[userCreated]("user.created")

	t.Run("delivers to every subscription", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)

		var mu sync.Mutex
		got := map[string]eventbus.Delivery[userCreated]{}
		done := make(chan struct{}, 2)
		record := func(name string) func(context.Context, eventbus.Delivery[userCreated]) error {
			return func(_ context.Context, d eventbus.Delivery[userCreated]) error {
				mu.Lock()
				got[name] = d
				mu.Unlock()
				done <- struct{}{}
				return nil
			}
		}
		eventbus.Subscribe(bus, evt, "send_welcome", record("send_welcome"))
		eventbus.Subscribe(bus, evt, "provision", record("provision"))

		svc, err := eventbus.NewService(bus, queue.WithConfig(fastConfig()))
		require.NoError(t, err)
		stop := runService(t, svc)
		defer stop()

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{Email: "a@b.c"}))

		for range 2 {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for deliveries")
			}
		}
		mu.Lock()
		defer mu.Unlock()
		require.Len(t, got, 2)
		assert.Equal(t, "a@b.c", got["send_welcome"].Payload.Email)
		assert.Equal(t, got["send_welcome"].ID, got["provision"].ID)
		assert.Equal(t, "user.created", got["provision"].Name)
	})

	t.Run("failing subscription retries without touching the healthy one", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)

		var attempts atomic.Int32
		flakyDone := make(chan struct{})
		eventbus.Subscribe(bus, evt, "flaky", func(context.Context, eventbus.Delivery[userCreated]) error {
			if attempts.Add(1) == 1 {
				return errors.New("transient")
			}
			close(flakyDone)
			return nil
		}, eventbus.WithRetryBackoff(backoffZero{}))
		healthy := make(chan struct{})
		eventbus.Subscribe(bus, evt, "healthy", func(context.Context, eventbus.Delivery[userCreated]) error {
			close(healthy)
			return nil
		})

		svc, err := eventbus.NewService(bus, queue.WithConfig(fastConfig()))
		require.NoError(t, err)
		stop := runService(t, svc)
		defer stop()

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))

		select {
		case <-flakyDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for retry")
		}
		assert.Equal(t, int32(2), attempts.Load())
		select {
		case <-healthy:
		case <-time.After(5 * time.Second):
			t.Fatal("healthy subscription never ran")
		}
	})

	t.Run("undecodable payload dead-letters as poison", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)
		eventbus.Subscribe(bus, evt, "strict", func(context.Context, eventbus.Delivery[userCreated]) error {
			t.Error("handler must not run on poison payload")
			return nil
		})

		// A foreign envelope version, a payload that does not decode into T,
		// and a missing event id must all dead-letter, not retry forever.
		require.NoError(t, broker.Push(context.Background(), queue.Job{
			ID: "0198f5c4-0000-7000-8000-000000000001", Queue: "user.created.strict", Type: "user.created.strict",
			Payload: []byte(`{"v":99,"id":"x","n":"user.created","at":1}`),
			RunAt:   time.Now().UTC().Add(-time.Second), CreatedAt: time.Now().UTC(),
		}, queue.Job{
			ID: "0198f5c4-0000-7000-8000-000000000002", Queue: "user.created.strict", Type: "user.created.strict",
			Payload: []byte(`{"v":1,"id":"x","n":"user.created","at":1,"p":[1,2,3]}`),
			RunAt:   time.Now().UTC().Add(-time.Second), CreatedAt: time.Now().UTC(),
		}, queue.Job{
			ID: "0198f5c4-0000-7000-8000-000000000003", Queue: "user.created.strict", Type: "user.created.strict",
			Payload: []byte(`{"v":1,"n":"user.created","at":1,"p":{}}`),
			RunAt:   time.Now().UTC().Add(-time.Second), CreatedAt: time.Now().UTC(),
		}))

		svc, err := eventbus.NewService(bus, queue.WithConfig(fastConfig()))
		require.NoError(t, err)
		stop := runService(t, svc)
		defer stop()

		require.Eventually(t, func() bool {
			dead, err := broker.ListDead(context.Background(), "user.created.strict", 10)
			return err == nil && len(dead) == 3
		}, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("subscription attempt budget dead-letters when spent", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)
		var attempts atomic.Int32
		eventbus.Subscribe(bus, evt, "budget", func(context.Context, eventbus.Delivery[userCreated]) error {
			attempts.Add(1)
			return errors.New("always fails")
		}, eventbus.WithMaxAttempts(1), eventbus.WithRetryBackoff(backoffZero{}))

		svc, err := eventbus.NewService(bus, queue.WithConfig(fastConfig()))
		require.NoError(t, err)
		stop := runService(t, svc)
		defer stop()

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))
		require.Eventually(t, func() bool {
			dead, err := broker.ListDead(context.Background(), "user.created.budget", 10)
			return err == nil && len(dead) == 1
		}, 5*time.Second, 10*time.Millisecond)
		assert.Equal(t, int32(1), attempts.Load())
	})

	t.Run("handler timeout takes the retry path", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)
		eventbus.Subscribe(bus, evt, "slow", func(ctx context.Context, _ eventbus.Delivery[userCreated]) error {
			<-ctx.Done()
			return ctx.Err()
		}, eventbus.WithHandlerTimeout(10*time.Millisecond), eventbus.WithMaxAttempts(1))

		svc, err := eventbus.NewService(bus, queue.WithConfig(fastConfig()))
		require.NoError(t, err)
		stop := runService(t, svc)
		defer stop()

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))
		require.Eventually(t, func() bool {
			dead, err := broker.ListDead(context.Background(), "user.created.slow", 10)
			return err == nil && len(dead) == 1
		}, 5*time.Second, 10*time.Millisecond)
	})
}

// backoffZero retries immediately (tests).
type backoffZero struct{}

func (backoffZero) Next(int) time.Duration { return 0 }
