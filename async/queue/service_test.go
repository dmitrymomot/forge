package queue_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

// testConfig returns worker knobs tuned for fast tests.
func testConfig() queue.Config {
	cfg := queue.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.Lease = 500 * time.Millisecond
	cfg.MaxAttempts = 25
	return cfg
}

// runService starts svc.Run in a goroutine and returns a stop func that
// cancels and waits for Run to return.
func runService(t *testing.T, svc *queue.Service) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				assert.ErrorIs(t, err, context.Canceled)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("service did not stop")
		}
	}
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, 5*time.Millisecond, msg)
}

func TestService_ProcessesTypedJob(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var got atomic.Value
	queue.Register(svc, kindWelcome, func(_ context.Context, p welcomePayload) error {
		got.Store(p)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u42"}))

	eventually(t, func() bool { return got.Load() != nil }, "handler must run")
	assert.Equal(t, welcomePayload{UserID: "u42"}, got.Load())
	eventually(t, func() bool {
		st, err := b.Stats(context.Background())
		return err == nil && st["default"].Pending == 0 && st["default"].Dead == 0
	}, "successful job must be acked away")
}

func TestService_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		if calls.Add(1) < 3 {
			return errors.New("transient")
		}
		return nil
	}, queue.WithHandlerBackoff(backoff.Constant(10*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool { return calls.Load() == 3 }, "handler must retry to the third attempt")
	eventually(t, func() bool {
		st, _ := b.Stats(context.Background())
		return st["default"].Pending == 0 && st["default"].Dead == 0
	}, "job must complete after retries")
}

func TestService_MaxAttemptsDeadLetters(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return errors.New("always fails")
	}, queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithMaxAttempts(2)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "job must dead-letter after max attempts")
	assert.EqualValues(t, 2, calls.Load(), "push-level WithMaxAttempts(2) bounds the attempts")
	dead, err := b.ListDead(context.Background(), "default", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)
	assert.Contains(t, dead[0].LastError, "always fails")
}

func TestService_HandlerMaxAttemptsOverride(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return errors.New("nope")
	}, queue.WithHandlerMaxAttempts(3), queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "job must dead-letter after handler max attempts")
	assert.EqualValues(t, 3, calls.Load())
}

func TestService_SkipRetryVerdict(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return queue.SkipRetry(errors.New("poison"))
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "SkipRetry must dead-letter immediately")
	assert.EqualValues(t, 1, calls.Load(), "no retries after SkipRetry")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	require.Len(t, dead, 1)
	assert.Contains(t, dead[0].LastError, "poison")
}

func TestService_CancelVerdict(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return queue.Cancel
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		st, _ := b.Stats(context.Background())
		return st["default"].Pending == 0
	}, "cancelled job must be acked away")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	assert.Empty(t, dead, "Cancel never dead-letters")
	assert.EqualValues(t, 1, calls.Load())
}

func TestService_PanicIsFailure(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		panic("boom")
	}, queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithMaxAttempts(2)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "panicking job must retry then dead-letter")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	require.Len(t, dead, 1)
	assert.Contains(t, dead[0].LastError, "panic")
}

func TestService_UnmarshalFailureDeadLetters(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	// user_id must be a string; number cannot unmarshal → poison.
	require.NoError(t, c.PushRaw(context.Background(), kindWelcome.Name(), []byte(`{"user_id":123}`)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "type-mismatched payload must dead-letter, not retry")
	assert.Zero(t, calls.Load(), "handler body must not run on unmarshal failure")
}

func TestService_UnregisteredKindDeadLetters(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, c.PushRaw(context.Background(), "nobody.home", []byte(`{}`)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "unregistered kind must dead-letter")
	dead, _ := b.ListDead(context.Background(), "default", 10)
	require.Len(t, dead, 1)
	assert.Contains(t, dead[0].LastError, "no handler")
}

func TestService_HandlerTimeout(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(ctx context.Context, _ welcomePayload) error {
		calls.Add(1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	}, queue.WithHandlerTimeout(30*time.Millisecond), queue.WithHandlerBackoff(backoff.Constant(5*time.Millisecond)))

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithMaxAttempts(2)))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "timed-out job must retry then dead-letter")
	assert.EqualValues(t, 2, calls.Load())
	dead, _ := b.ListDead(context.Background(), "default", 10)
	require.Len(t, dead, 1)
	assert.Contains(t, dead[0].LastError, "context deadline exceeded")
}

type scopeKey struct{}

func TestService_ScopeRestoredIntoHandlerContext(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b,
		queue.WithConfig(testConfig()),
		queue.WithScopeContext(func(ctx context.Context, scope string) context.Context {
			return context.WithValue(ctx, scopeKey{}, scope)
		}),
	)
	require.NoError(t, err)

	var got atomic.Value
	queue.Register(svc, kindWelcome, func(ctx context.Context, _ welcomePayload) error {
		if s, ok := ctx.Value(scopeKey{}).(string); ok {
			got.Store(s)
		}
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) { return "tenant-a", nil }))
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool { return got.Load() != nil }, "handler must run")
	assert.Equal(t, "tenant-a", got.Load())
}

func TestService_ScopeMissingFailsClosed(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b,
		queue.WithConfig(testConfig()),
		queue.WithScopeContext(func(ctx context.Context, _ string) context.Context { return ctx }),
	)
	require.NoError(t, err)

	var calls atomic.Int32
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	// Client WITHOUT a scope hook feeding a scoped worker: fail closed.
	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	eventually(t, func() bool {
		dead, _ := b.ListDead(context.Background(), "default", 10)
		return len(dead) == 1
	}, "unscoped job on a scoped worker must dead-letter")
	assert.Zero(t, calls.Load())
	dead, _ := b.ListDead(context.Background(), "default", 10)
	require.Len(t, dead, 1)
	assert.Contains(t, dead[0].LastError, "scope missing")
}

func TestService_HeartbeatKeepsLongJobClaimed(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Lease = 100 * time.Millisecond // handler runs 5x the lease
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(cfg))
	require.NoError(t, err)

	var calls atomic.Int32
	done := make(chan struct{})
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		calls.Add(1)
		time.Sleep(500 * time.Millisecond)
		close(done)
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never finished")
	}
	time.Sleep(50 * time.Millisecond) // allow ack
	assert.EqualValues(t, 1, calls.Load(), "heartbeat must prevent redelivery of a running job")
	st, _ := b.Stats(context.Background())
	assert.Zero(t, st["default"].Pending)
}

func TestService_DrainWaitsForInflight(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(testConfig()))
	require.NoError(t, err)

	started := make(chan struct{})
	var finished atomic.Bool
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		close(started)
		time.Sleep(300 * time.Millisecond)
		finished.Store(true)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	c := queue.NewClient(b)
	require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))
	<-started
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	assert.True(t, finished.Load(), "Run must not return before in-flight handlers finish")
	st, _ := b.Stats(context.Background())
	assert.Zero(t, st["default"].Pending, "drained job must still be acked (WithoutCancel op ctx)")
}

func TestService_ConcurrencyBound(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	b := queue.NewMemoryBroker()
	svc, err := queue.NewService(b, queue.WithConfig(cfg), queue.WithConcurrency(2))
	require.NoError(t, err)

	var mu sync.Mutex
	inflight, peak := 0, 0
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
		return nil
	})

	stop := runService(t, svc)
	defer stop()

	c := queue.NewClient(b)
	for range 8 {
		require.NoError(t, queue.Push(context.Background(), c, kindWelcome, welcomePayload{UserID: "u"}))
	}

	eventually(t, func() bool {
		st, _ := b.Stats(context.Background())
		return st["default"].Pending == 0
	}, "all jobs must complete")
	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, peak, 2, "concurrency bound must hold")
}

func TestService_NameAndValidation(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()

	svc, err := queue.NewService(b)
	require.NoError(t, err)
	assert.Equal(t, "queue", svc.Name())

	svc2, err := queue.NewService(b, queue.WithName("queue-video"))
	require.NoError(t, err)
	assert.Equal(t, "queue-video", svc2.Name())

	_, err = queue.NewService(b, queue.WithConcurrency(-1))
	assert.ErrorIs(t, err, queue.ErrInvalidConfig)

	_, err = queue.NewService(b, queue.WithQueues(map[string]int{"a": 0}))
	assert.ErrorIs(t, err, queue.ErrInvalidConfig)

	_, err = queue.NewService(nil)
	assert.ErrorIs(t, err, queue.ErrInvalidConfig)
}

func TestRegister_DuplicatePanics(t *testing.T) {
	t.Parallel()
	svc, err := queue.NewService(queue.NewMemoryBroker())
	require.NoError(t, err)
	queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })
	assert.Panics(t, func() {
		queue.Register(svc, kindWelcome, func(context.Context, welcomePayload) error { return nil })
	})
	assert.Panics(t, func() {
		queue.Register(svc, queue.NewKind[welcomePayload]("other.kind"), nil)
	})
}
