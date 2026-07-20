package scheduler_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/scheduler"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/supervisor"
)

var _ supervisor.Service = (*scheduler.Scheduler)(nil)

type tickPayload struct {
	Job  string    `json:"job"`
	Tick time.Time `json:"tick"`
}

var kindTick = queue.NewKind[tickPayload]("scheduler_test.tick")

// fastConfig keeps test retries snappy.
func fastConfig() scheduler.Config {
	cfg := scheduler.DefaultConfig()
	cfg.RetryInterval = 20 * time.Millisecond
	return cfg
}

// runFor runs sched for d in the background and returns after it stopped.
func runFor(t *testing.T, sched *scheduler.Scheduler, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	err := sched.Run(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// drain claims jobs off broker until want arrived or timeout passed.
func drain(t *testing.T, broker queue.Broker, want int, timeout time.Duration) []queue.ClaimedJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var jobs []queue.ClaimedJob
	for {
		claimed, err := broker.Claim(context.Background(), "default", 100, time.Minute)
		require.NoError(t, err)
		jobs = append(jobs, claimed...)
		if len(jobs) >= want || time.Now().After(deadline) {
			return jobs
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func decodeTicks(t *testing.T, jobs []queue.ClaimedJob) []tickPayload {
	t.Helper()
	out := make([]tickPayload, 0, len(jobs))
	for _, j := range jobs {
		require.Equal(t, kindTick.Name(), j.Type)
		var p tickPayload
		require.NoError(t, json.Unmarshal(j.Payload, &p))
		out = append(out, p)
	}
	return out
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	client := queue.NewClient(queue.NewMemoryBroker())

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		_, err := scheduler.New(nil)
		require.ErrorIs(t, err, scheduler.ErrInvalidConfig)
	})

	t.Run("nil store", func(t *testing.T) {
		t.Parallel()
		_, err := scheduler.New(client, scheduler.WithStore(nil))
		require.ErrorIs(t, err, scheduler.ErrInvalidConfig)
	})

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		_, err := scheduler.New(client, scheduler.WithName(""))
		require.ErrorIs(t, err, scheduler.ErrInvalidConfig)
	})

	t.Run("nil logger", func(t *testing.T) {
		t.Parallel()
		_, err := scheduler.New(client, scheduler.WithLogger(nil))
		require.ErrorIs(t, err, scheduler.ErrInvalidConfig)
	})

	t.Run("nil clock", func(t *testing.T) {
		t.Parallel()
		_, err := scheduler.New(client, scheduler.WithClock(nil))
		require.ErrorIs(t, err, scheduler.ErrInvalidConfig)
	})

	t.Run("bad config", func(t *testing.T) {
		t.Parallel()
		_, err := scheduler.New(client, scheduler.WithConfig(scheduler.Config{}))
		require.ErrorIs(t, err, scheduler.ErrInvalidConfig)
	})

	t.Run("defaults valid", func(t *testing.T) {
		t.Parallel()
		s, err := scheduler.New(client)
		require.NoError(t, err)
		assert.Equal(t, "scheduler", s.Name())
	})

	t.Run("name override", func(t *testing.T) {
		t.Parallel()
		s, err := scheduler.New(client, scheduler.WithName("scheduler-eu"))
		require.NoError(t, err)
		assert.Equal(t, "scheduler-eu", s.Name())
	})
}

func TestAddPanics(t *testing.T) {
	t.Parallel()

	newScheduler := func(t *testing.T) *scheduler.Scheduler {
		t.Helper()
		s, err := scheduler.New(queue.NewClient(queue.NewMemoryBroker()))
		require.NoError(t, err)
		return s
	}

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		s := newScheduler(t)
		assert.Panics(t, func() { scheduler.Add(s, "", scheduler.Every(time.Hour), kindTick, tickPayload{}) })
	})

	t.Run("nil schedule", func(t *testing.T) {
		t.Parallel()
		s := newScheduler(t)
		assert.Panics(t, func() { scheduler.Add(s, "job", nil, kindTick, tickPayload{}) })
	})

	t.Run("nil build func", func(t *testing.T) {
		t.Parallel()
		s := newScheduler(t)
		assert.Panics(t, func() { scheduler.AddFunc(s, "job", scheduler.Every(time.Hour), kindTick, nil) })
	})

	t.Run("duplicate name", func(t *testing.T) {
		t.Parallel()
		s := newScheduler(t)
		scheduler.Add(s, "job", scheduler.Every(time.Hour), kindTick, tickPayload{})
		assert.Panics(t, func() { scheduler.Add(s, "job", scheduler.Every(time.Minute), kindTick, tickPayload{}) })
	})

	t.Run("after run started", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		s, err := scheduler.New(queue.NewClient(broker), scheduler.WithConfig(fastConfig()))
		require.NoError(t, err)
		scheduler.Add(s, "job", scheduler.Every(5*time.Millisecond), kindTick, tickPayload{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { _ = s.Run(ctx); close(done) }()
		// A job on the broker proves Run is past its startup guard.
		require.NotEmpty(t, drain(t, broker, 1, time.Second))
		assert.Panics(t, func() { scheduler.Add(s, "late", scheduler.Every(time.Hour), kindTick, tickPayload{}) })
		cancel()
		<-done
	})
}

func TestRunGuards(t *testing.T) {
	t.Parallel()

	t.Run("no jobs", func(t *testing.T) {
		t.Parallel()
		s, err := scheduler.New(queue.NewClient(queue.NewMemoryBroker()))
		require.NoError(t, err)
		require.ErrorIs(t, s.Run(context.Background()), scheduler.ErrNoJobs)
	})

	t.Run("second run rejected", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		s, err := scheduler.New(queue.NewClient(broker), scheduler.WithConfig(fastConfig()))
		require.NoError(t, err)
		scheduler.Add(s, "job", scheduler.Every(5*time.Millisecond), kindTick, tickPayload{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { _ = s.Run(ctx); close(done) }()
		// A job on the broker proves Run is past its startup guard.
		require.NotEmpty(t, drain(t, broker, 1, time.Second))
		require.ErrorIs(t, s.Run(context.Background()), scheduler.ErrAlreadyRunning)
		cancel()
		<-done
	})

	t.Run("cancel stops a parked run", func(t *testing.T) {
		t.Parallel()
		s, err := scheduler.New(queue.NewClient(queue.NewMemoryBroker()))
		require.NoError(t, err)
		scheduler.Add(s, "job", scheduler.Every(time.Hour), kindTick, tickPayload{})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()
		time.Sleep(30 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("Run did not stop on cancel")
		}
	})
}

func TestFiresOnSchedule(t *testing.T) {
	t.Parallel()

	broker := queue.NewMemoryBroker()
	s, err := scheduler.New(queue.NewClient(broker), scheduler.WithConfig(fastConfig()))
	require.NoError(t, err)
	scheduler.Add(s, "report.tick", scheduler.Every(50*time.Millisecond), kindTick, tickPayload{Job: "report.tick"})

	runFor(t, s, 300*time.Millisecond)

	jobs := drain(t, broker, 2, time.Second)
	require.GreaterOrEqual(t, len(jobs), 2)
	for _, p := range decodeTicks(t, jobs) {
		assert.Equal(t, "report.tick", p.Job)
	}
}

func TestAddFuncReceivesTick(t *testing.T) {
	t.Parallel()

	broker := queue.NewMemoryBroker()
	s, err := scheduler.New(queue.NewClient(broker), scheduler.WithConfig(fastConfig()))
	require.NoError(t, err)
	scheduler.AddFunc(s, "report.tick", scheduler.Every(50*time.Millisecond), kindTick,
		func(scheduledFor time.Time) (tickPayload, error) {
			return tickPayload{Job: "report.tick", Tick: scheduledFor}, nil
		})

	runFor(t, s, 300*time.Millisecond)

	ticks := decodeTicks(t, drain(t, broker, 2, time.Second))
	require.GreaterOrEqual(t, len(ticks), 2)
	seen := make(map[int64]struct{}, len(ticks))
	for _, p := range ticks {
		require.False(t, p.Tick.IsZero())
		assert.Zero(t, p.Tick.UnixNano()%int64(50*time.Millisecond), "tick %v not aligned to the interval", p.Tick)
		_, dup := seen[p.Tick.UnixNano()]
		assert.False(t, dup, "tick %v fired twice", p.Tick)
		seen[p.Tick.UnixNano()] = struct{}{}
	}
}

func TestFleetFiresOncePerTick(t *testing.T) {
	t.Parallel()

	broker := queue.NewMemoryBroker()
	store := scheduler.NewMemoryStore()

	newInstance := func(t *testing.T) *scheduler.Scheduler {
		t.Helper()
		s, err := scheduler.New(queue.NewClient(broker), scheduler.WithStore(store), scheduler.WithConfig(fastConfig()))
		require.NoError(t, err)
		scheduler.AddFunc(s, "report.tick", scheduler.Every(50*time.Millisecond), kindTick,
			func(scheduledFor time.Time) (tickPayload, error) {
				return tickPayload{Job: "report.tick", Tick: scheduledFor}, nil
			})
		return s
	}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	for range 3 {
		s := newInstance(t)
		wg.Go(func() { _ = s.Run(ctx) })
	}
	wg.Wait()

	ticks := decodeTicks(t, drain(t, broker, 2, time.Second))
	require.GreaterOrEqual(t, len(ticks), 2)
	seen := make(map[int64]struct{}, len(ticks))
	for _, p := range ticks {
		_, dup := seen[p.Tick.UnixNano()]
		require.False(t, dup, "tick %v enqueued by more than one instance", p.Tick)
		seen[p.Tick.UnixNano()] = struct{}{}
	}
}

func TestNoCatchUpAtStart(t *testing.T) {
	t.Parallel()

	broker := queue.NewMemoryBroker()
	s, err := scheduler.New(queue.NewClient(broker))
	require.NoError(t, err)
	// Hourly ticks certainly "missed" before start are not replayed.
	scheduler.Add(s, "report.hourly", scheduler.Every(time.Hour), kindTick, tickPayload{})

	runFor(t, s, 150*time.Millisecond)

	assert.Empty(t, drain(t, broker, 1, 50*time.Millisecond))
}

// flakyBroker fails Push while failing is set; everything else delegates.
type flakyBroker struct {
	*queue.MemoryBroker
	failing atomic.Bool
}

func (b *flakyBroker) Push(ctx context.Context, jobs ...queue.Job) error {
	if b.failing.Load() {
		return errors.New("broker down")
	}
	return b.MemoryBroker.Push(ctx, jobs...)
}

func TestRetriesFailedTick(t *testing.T) {
	t.Parallel()

	broker := &flakyBroker{MemoryBroker: queue.NewMemoryBroker()}
	broker.failing.Store(true)
	store := scheduler.NewMemoryStore()
	s, err := scheduler.New(queue.NewClient(broker), scheduler.WithStore(store), scheduler.WithConfig(fastConfig()))
	require.NoError(t, err)
	scheduler.AddFunc(s, "report.tick", scheduler.Every(50*time.Millisecond), kindTick,
		func(scheduledFor time.Time) (tickPayload, error) {
			return tickPayload{Job: "report.tick", Tick: scheduledFor}, nil
		})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = s.Run(ctx); close(done) }()

	// While the broker is down nothing is enqueued and claims are released.
	time.Sleep(150 * time.Millisecond)
	require.Empty(t, drain(t, broker, 1, 20*time.Millisecond))

	// After recovery the pending tick fires without waiting for a new one.
	broker.failing.Store(false)
	jobs := drain(t, broker, 1, time.Second)
	require.NotEmpty(t, jobs, "tick was not retried after the broker recovered")
	<-done
}

type scopeKey struct{}

func TestPushContextScopesJobs(t *testing.T) {
	t.Parallel()

	broker := queue.NewMemoryBroker()
	client := queue.NewClient(broker, queue.WithScope(func(ctx context.Context) (string, error) {
		scope, _ := ctx.Value(scopeKey{}).(string)
		if scope == "" {
			return "", errors.New("no tenant in context")
		}
		return scope, nil
	}))
	s, err := scheduler.New(client,
		scheduler.WithConfig(fastConfig()),
		scheduler.WithPushContext(func(ctx context.Context) context.Context {
			return context.WithValue(ctx, scopeKey{}, "system")
		}))
	require.NoError(t, err)
	scheduler.Add(s, "report.tick", scheduler.Every(50*time.Millisecond), kindTick, tickPayload{Job: "report.tick"})

	runFor(t, s, 200*time.Millisecond)

	jobs := drain(t, broker, 1, time.Second)
	require.NotEmpty(t, jobs)
	for _, j := range jobs {
		assert.Equal(t, "system", j.Scope)
	}
}

func TestSweepPurgesOldClaims(t *testing.T) {
	t.Parallel()

	base := at("2026-07-20T10:00:00Z")
	store := scheduler.NewMemoryStore()
	ctx := context.Background()
	oldTick := base.Add(-2 * time.Hour)
	freshTick := base.Add(-30 * time.Minute)
	require.NoError(t, store.Claim(ctx, "old", oldTick))
	require.NoError(t, store.Claim(ctx, "fresh", freshTick))

	cfg := fastConfig()
	cfg.Retention = time.Hour
	cfg.SweepInterval = 20 * time.Millisecond
	s, err := scheduler.New(queue.NewClient(queue.NewMemoryBroker()),
		scheduler.WithStore(store), scheduler.WithConfig(cfg), scheduler.WithClock(clock.NewMock(base)))
	require.NoError(t, err)
	scheduler.Add(s, "idle", scheduler.Every(10*time.Hour), kindTick, tickPayload{})

	runFor(t, s, 150*time.Millisecond)

	// The stale claim was purged (claimable again); the fresh one survived.
	require.NoError(t, store.Claim(ctx, "old", oldTick))
	require.ErrorIs(t, store.Claim(ctx, "fresh", freshTick), scheduler.ErrAlreadyClaimed)
}

func TestBuildErrorRetries(t *testing.T) {
	t.Parallel()

	broker := queue.NewMemoryBroker()
	var calls atomic.Int32
	s, err := scheduler.New(queue.NewClient(broker), scheduler.WithConfig(fastConfig()))
	require.NoError(t, err)
	scheduler.AddFunc(s, "report.tick", scheduler.Every(50*time.Millisecond), kindTick,
		func(scheduledFor time.Time) (tickPayload, error) {
			if calls.Add(1) == 1 {
				return tickPayload{}, errors.New("upstream not ready")
			}
			return tickPayload{Job: "report.tick", Tick: scheduledFor}, nil
		})

	runFor(t, s, 300*time.Millisecond)

	require.NotEmpty(t, drain(t, broker, 1, time.Second), "tick was not retried after a build error")
	assert.GreaterOrEqual(t, calls.Load(), int32(2))
}
