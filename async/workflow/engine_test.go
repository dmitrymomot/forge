package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/workflow"
	"github.com/dmitrymomot/forge/resilience/backoff"
)

type state struct {
	Log []string `json:"log"`
}

// recorder counts step invocations outside the checkpointed state, so tests
// can tell "ran again after redelivery" from "resumed past the checkpoint".
type recorder struct {
	mu    sync.Mutex
	calls map[string]int
}

func newRecorder() *recorder { return &recorder{calls: make(map[string]int)} }

func (r *recorder) hit(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[name]++
}

func (r *recorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[name]
}

// step returns a Step that records its invocation, appends its name to the
// state log, and delegates the verdict to fn (nil fn always succeeds).
func step(rec *recorder, name string, fn func(calls int) error, comp func(ctx context.Context, s *state) error) workflow.Step[state] {
	return workflow.Step[state]{
		Name: name,
		Run: func(_ context.Context, s *state) error {
			rec.hit(name)
			if fn != nil {
				if err := fn(rec.count(name)); err != nil {
					return err
				}
			}
			s.Log = append(s.Log, name)
			return nil
		},
		Compensate: comp,
	}
}

// comp returns a compensation that records its invocation and appends
// "undo:<name>" to the state log; fn (optional) can veto with an error.
func comp(rec *recorder, name string, fn func(calls int) error) func(ctx context.Context, s *state) error {
	key := "undo:" + name
	return func(_ context.Context, s *state) error {
		rec.hit(key)
		if fn != nil {
			if err := fn(rec.count(key)); err != nil {
				return err
			}
		}
		s.Log = append(s.Log, key)
		return nil
	}
}

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
	stop := func() {
		cancel()
		<-stopped
	}
	t.Cleanup(stop)
	return stop
}

// startAndDrive registers wf on a fresh engine over a memory broker+store,
// starts the worker, and starts one run.
func startAndDrive(t *testing.T, wf *workflow.Workflow[state], regOpts ...workflow.RegisterOption) (*workflow.MemoryStore, *queue.Client, string) {
	t.Helper()
	broker := queue.NewMemoryBroker()
	store := workflow.NewMemoryStore()
	eng := workflow.NewEngine(broker, store)
	regOpts = append([]workflow.RegisterOption{workflow.WithRetryBackoff(backoff.Constant(0))}, regOpts...)
	workflow.Register(eng, wf, regOpts...)
	svc, err := workflow.NewService(eng, queue.WithConfig(fastConfig()))
	require.NoError(t, err)
	runService(t, svc)
	runID, err := workflow.Start(context.Background(), eng, wf, state{})
	require.NoError(t, err)
	return store, queue.NewClient(broker), runID
}

func waitStatus(t *testing.T, store workflow.Store, runID string, want workflow.Status) workflow.Run {
	t.Helper()
	var run workflow.Run
	require.Eventually(t, func() bool {
		r, err := store.Get(context.Background(), runID)
		if err != nil {
			return false
		}
		run = r
		return run.Status == want
	}, 5*time.Second, 5*time.Millisecond, "run %s never reached %s", runID, want)
	return run
}

func decodeLog(t *testing.T, run workflow.Run) []string {
	t.Helper()
	var s state
	require.NoError(t, json.Unmarshal(run.State, &s))
	return s.Log
}

func TestNewEngine(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { workflow.NewEngine(nil, workflow.NewMemoryStore()) })
	assert.Panics(t, func() { workflow.NewEngine(queue.NewMemoryBroker(), nil) })
}

func TestRegister(t *testing.T) {
	t.Parallel()
	eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore())
	wf := workflow.New("wf.dup", workflow.Step[state]{Name: "a", Run: func(context.Context, *state) error { return nil }})
	workflow.Register(eng, wf)
	assert.Panics(t, func() { workflow.Register(eng, wf) })
	assert.Panics(t, func() { workflow.Register[state](eng, nil) })
}

func TestNewService(t *testing.T) {
	t.Parallel()
	eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore())
	_, err := workflow.NewService(eng)
	assert.ErrorIs(t, err, workflow.ErrNoWorkflows)

	wf := workflow.New("wf.svc", workflow.Step[state]{Name: "a", Run: func(context.Context, *state) error { return nil }})
	workflow.Register(eng, wf)
	svc, err := workflow.NewService(eng)
	require.NoError(t, err)
	assert.Equal(t, "workflow", svc.Name())
}

func TestStart_Validation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("unregistered workflow", func(t *testing.T) {
		t.Parallel()
		eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore())
		wf := workflow.New("wf.unreg", workflow.Step[state]{Name: "a", Run: func(context.Context, *state) error { return nil }})
		_, err := workflow.Start(ctx, eng, wf, state{})
		assert.ErrorIs(t, err, workflow.ErrNotRegistered)
	})

	t.Run("different definition than registered", func(t *testing.T) {
		t.Parallel()
		eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore())
		run := func(context.Context, *state) error { return nil }
		registered := workflow.New("wf.twin", workflow.Step[state]{Name: "a", Run: run})
		impostor := workflow.New("wf.twin", workflow.Step[state]{Name: "b", Run: run})
		workflow.Register(eng, registered)
		_, err := workflow.Start(ctx, eng, impostor, state{})
		assert.ErrorIs(t, err, workflow.ErrNotRegistered)
	})

	t.Run("duplicate run id", func(t *testing.T) {
		t.Parallel()
		eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore())
		wf := workflow.New("wf.dupid", workflow.Step[state]{Name: "a", Run: func(context.Context, *state) error { return nil }})
		workflow.Register(eng, wf)
		_, err := workflow.Start(ctx, eng, wf, state{}, workflow.WithRunID("onboard:u1"))
		require.NoError(t, err)
		_, err = workflow.Start(ctx, eng, wf, state{}, workflow.WithRunID("onboard:u1"))
		assert.ErrorIs(t, err, workflow.ErrRunAlreadyExists)
	})

	t.Run("scope fail-closed", func(t *testing.T) {
		t.Parallel()
		eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore(),
			workflow.WithScope(func(context.Context) (string, error) { return "", nil }))
		wf := workflow.New("wf.scope", workflow.Step[state]{Name: "a", Run: func(context.Context, *state) error { return nil }})
		workflow.Register(eng, wf)
		_, err := workflow.Start(ctx, eng, wf, state{})
		assert.ErrorIs(t, err, workflow.ErrScopeMissing)
	})

	t.Run("push failure marks the run failed", func(t *testing.T) {
		t.Parallel()
		store := workflow.NewMemoryStore()
		eng := workflow.NewEngine(&failingPushBroker{Broker: queue.NewMemoryBroker()}, store)
		wf := workflow.New("wf.pushfail", workflow.Step[state]{Name: "a", Run: func(context.Context, *state) error { return nil }})
		workflow.Register(eng, wf)
		_, err := workflow.Start(ctx, eng, wf, state{}, workflow.WithRunID("r1"))
		require.Error(t, err)

		run, gerr := store.Get(ctx, "r1")
		require.NoError(t, gerr)
		assert.Equal(t, workflow.StatusFailed, run.Status)
		assert.Contains(t, run.Error, "enqueue failed")
	})
}

type failingPushBroker struct {
	queue.Broker
}

func (b *failingPushBroker) Push(context.Context, ...queue.Job) error {
	return errors.New("broker down")
}

func TestRun_CompletesAndCheckpointsState(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.happy",
		step(rec, "a", nil, nil),
		step(rec, "b", nil, nil),
		step(rec, "c", nil, nil),
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusCompleted)

	assert.Equal(t, []string{"a", "b", "c"}, decodeLog(t, run))
	assert.Equal(t, 1, rec.count("a"))
	assert.Equal(t, 1, rec.count("b"))
	assert.Equal(t, 1, rec.count("c"))
	assert.Equal(t, 3, run.Step)
	assert.Empty(t, run.Error)
}

func TestRun_TransientFailureResumesFromCheckpoint(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.retry",
		step(rec, "a", nil, nil),
		step(rec, "b", func(calls int) error {
			if calls <= 2 {
				return fmt.Errorf("transient %d", calls)
			}
			return nil
		}, nil),
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusCompleted)

	assert.Equal(t, []string{"a", "b"}, decodeLog(t, run))
	assert.Equal(t, 1, rec.count("a"), "checkpointed step must not re-run on retry")
	assert.Equal(t, 3, rec.count("b"))
}

func TestRun_FailVerdictCompensatesInReverse(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.saga",
		step(rec, "debit", nil, comp(rec, "debit", nil)),
		step(rec, "hold", nil, nil), // no compensation: must be skipped
		step(rec, "transfer", nil, comp(rec, "transfer", nil)),
		step(rec, "notify", func(int) error {
			return workflow.Fail(errors.New("recipient rejected"))
		}, comp(rec, "notify", nil)),
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusFailed)

	assert.Equal(t, []string{"debit", "hold", "transfer", "undo:transfer", "undo:debit"}, decodeLog(t, run))
	assert.Equal(t, 0, rec.count("undo:notify"), "the failing step never completed, so it must not compensate")
	assert.Contains(t, run.Error, "recipient rejected")
	assert.Equal(t, 1, rec.count("notify"), "Fail is permanent: no retries")
}

func TestRun_AttemptBudgetExhaustionCompensates(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.budget",
		step(rec, "a", nil, comp(rec, "a", nil)),
		workflow.Step[state]{
			Name:        "b",
			MaxAttempts: 2,
			Run: func(context.Context, *state) error {
				rec.hit("b")
				return errors.New("always down")
			},
		},
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusFailed)

	assert.Equal(t, 2, rec.count("b"), "budget of 2 means exactly 2 attempts")
	assert.Equal(t, []string{"a", "undo:a"}, decodeLog(t, run))
	assert.Contains(t, run.Error, "always down")
}

func TestRun_PanicBurnsAttempts(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.panic",
		step(rec, "a", nil, comp(rec, "a", nil)),
		workflow.Step[state]{
			Name:        "b",
			MaxAttempts: 1,
			Run: func(context.Context, *state) error {
				rec.hit("b")
				panic("boom")
			},
		},
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusFailed)

	assert.Equal(t, 1, rec.count("b"))
	assert.Equal(t, []string{"a", "undo:a"}, decodeLog(t, run))
	assert.Contains(t, run.Error, "panic")
}

func TestRun_NoCompensationsFailsDirectly(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.nocomp",
		step(rec, "a", nil, nil),
		step(rec, "b", func(int) error { return workflow.Fail(errors.New("dead end")) }, nil),
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusFailed)
	assert.Equal(t, []string{"a"}, decodeLog(t, run))
	assert.Contains(t, run.Error, "dead end")
}

func TestRun_QueueVerdictsFromStepsArePermanent(t *testing.T) {
	t.Parallel()
	for name, verdict := range map[string]error{
		"skip_retry": queue.SkipRetry(errors.New("poison")),
		"cancel":     queue.Cancel,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := newRecorder()
			wf := workflow.New("wf.verdict."+name,
				step(rec, "a", nil, comp(rec, "a", nil)),
				step(rec, "b", func(int) error { return verdict }, nil),
			)
			store, client, runID := startAndDrive(t, wf)
			run := waitStatus(t, store, runID, workflow.StatusFailed)

			assert.Equal(t, 1, rec.count("b"))
			assert.Equal(t, []string{"a", "undo:a"}, decodeLog(t, run))
			dead, err := client.ListDead(context.Background(), wf.Name(), 10)
			require.NoError(t, err)
			assert.Empty(t, dead, "a permanent step failure compensates; it must not dead-letter the driving job")
		})
	}
}

func TestRun_CompensationRetriesTransiently(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.compretry",
		step(rec, "a", nil, comp(rec, "a", func(calls int) error {
			if calls == 1 {
				return errors.New("undo hiccup")
			}
			return nil
		})),
		step(rec, "b", func(int) error { return workflow.Fail(errors.New("nope")) }, nil),
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusFailed)

	assert.Equal(t, 2, rec.count("undo:a"))
	assert.Equal(t, []string{"a", "undo:a"}, decodeLog(t, run))
}

func TestRun_CompensationExhaustionDeadLettersAndRequeueResumes(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	var allow sync.Map // flips to true when the test un-breaks the compensation
	wf := workflow.New("wf.compdead",
		workflow.Step[state]{
			Name:        "a",
			MaxAttempts: 2,
			Run: func(_ context.Context, s *state) error {
				rec.hit("a")
				s.Log = append(s.Log, "a")
				return nil
			},
			Compensate: func(_ context.Context, s *state) error {
				rec.hit("undo:a")
				if _, ok := allow.Load("ok"); !ok {
					return errors.New("undo broken")
				}
				s.Log = append(s.Log, "undo:a")
				return nil
			},
		},
		step(rec, "b", func(int) error { return workflow.Fail(errors.New("nope")) }, nil),
	)
	store, client, runID := startAndDrive(t, wf)

	// The compensation burns its budget and the driving job dead-letters,
	// with the run parked compensating and its attempt counter reset.
	require.Eventually(t, func() bool {
		jobs, lerr := client.ListDead(context.Background(), wf.Name(), 10)
		require.NoError(t, lerr)
		return len(jobs) == 1
	}, 5*time.Second, 5*time.Millisecond)
	dead, err := client.ListDead(context.Background(), wf.Name(), 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)
	run, err := store.Get(context.Background(), runID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusCompensating, run.Status)
	assert.Equal(t, 0, run.Attempt, "attempt counter must reset so a requeue gets a fresh budget")
	assert.Equal(t, 2, rec.count("undo:a"))

	// Requeue resumes the unwind from the checkpoint.
	allow.Store("ok", true)
	require.NoError(t, client.Requeue(context.Background(), dead[0].ID))
	run = waitStatus(t, store, runID, workflow.StatusFailed)
	assert.Equal(t, []string{"a", "undo:a"}, decodeLog(t, run))
}

func TestRun_DuplicateDeliveryOfFinishedRunIsNoop(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.dupdelivery", step(rec, "a", nil, nil))
	broker := queue.NewMemoryBroker()
	store := workflow.NewMemoryStore()
	eng := workflow.NewEngine(broker, store)
	workflow.Register(eng, wf)
	svc, err := workflow.NewService(eng, queue.WithConfig(fastConfig()))
	require.NoError(t, err)
	runService(t, svc)

	runID, err := workflow.Start(context.Background(), eng, wf, state{})
	require.NoError(t, err)
	waitStatus(t, store, runID, workflow.StatusCompleted)

	// Redeliver the envelope by hand — the at-least-once case.
	client := queue.NewClient(broker)
	require.NoError(t, client.PushRaw(context.Background(), wf.Name(),
		fmt.Appendf(nil, `{"run_id":%q,"v":1}`, runID), queue.WithQueue(wf.Name())))
	require.Eventually(t, func() bool {
		stats, serr := client.Stats(context.Background())
		require.NoError(t, serr)
		return stats[wf.Name()].Pending == 0
	}, 5*time.Second, 5*time.Millisecond)

	assert.Equal(t, 1, rec.count("a"), "a finished run must not re-run steps on duplicate delivery")
	run, err := store.Get(context.Background(), runID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusCompleted, run.Status)
}

func TestRun_FailedStepMutationsDoNotLeakIntoCompensation(t *testing.T) {
	t.Parallel()
	rec := newRecorder()
	wf := workflow.New("wf.stateleak",
		step(rec, "a", nil, comp(rec, "a", nil)),
		workflow.Step[state]{
			Name:        "b",
			MaxAttempts: 1,
			Run: func(_ context.Context, s *state) error {
				s.Log = append(s.Log, "b-partial") // mutates, then fails
				return errors.New("late failure")
			},
		},
	)
	store, _, runID := startAndDrive(t, wf)
	run := waitStatus(t, store, runID, workflow.StatusFailed)

	assert.Equal(t, []string{"a", "undo:a"}, decodeLog(t, run), "the failed step's partial mutation must not survive")
}

func TestRun_ScopeIsCapturedAndRestored(t *testing.T) {
	t.Parallel()
	type ctxKey struct{}
	var seen sync.Map
	rec := newRecorder()
	wf := workflow.New("wf.tenant",
		workflow.Step[state]{Name: "a", Run: func(ctx context.Context, _ *state) error {
			rec.hit("a")
			if v, ok := ctx.Value(ctxKey{}).(string); ok {
				seen.Store("scope", v)
			}
			return nil
		}},
	)
	broker := queue.NewMemoryBroker()
	store := workflow.NewMemoryStore()
	eng := workflow.NewEngine(broker, store,
		workflow.WithScope(func(context.Context) (string, error) { return "tenant-1", nil }))
	workflow.Register(eng, wf)
	svc, err := workflow.NewService(eng, queue.WithConfig(fastConfig()),
		queue.WithScopeContext(func(ctx context.Context, scope string) context.Context {
			return context.WithValue(ctx, ctxKey{}, scope)
		}))
	require.NoError(t, err)
	runService(t, svc)

	runID, err := workflow.Start(context.Background(), eng, wf, state{})
	require.NoError(t, err)
	run := waitStatus(t, store, runID, workflow.StatusCompleted)

	assert.Equal(t, "tenant-1", run.Scope)
	got, ok := seen.Load("scope")
	require.True(t, ok)
	assert.Equal(t, "tenant-1", got)
}
