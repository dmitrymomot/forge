package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/logger"
)

// wireVersion versions the driving-job envelope for cross-deploy safety.
const wireVersion = 1

// runAttemptMargin pads the driving job's attempt budget beyond the sum of
// step budgets: invocations that burn a job attempt without burning a step
// attempt (store outages, shutdown or lease loss mid-step) must not
// dead-letter a run that is still making progress.
const runAttemptMargin = 10

// runEnvelope is the driving job's payload: everything else lives in the
// Store, so redeliveries always see the freshest checkpoint.
type runEnvelope struct {
	RunID string `json:"run_id"`
	V     int    `json:"v"`
}

// registration is one workflow's type-erased wiring on the engine.
type registration struct {
	def            any // the *Workflow[S], identity-checked by Start
	handle         func(ctx context.Context, env runEnvelope) error
	hopts          []queue.HandlerOption
	jobMaxAttempts int
}

// Engine wires workflows to a queue broker and a checkpoint Store. Wire it
// once at startup: Register every workflow, then share it app-wide — Start is
// safe for concurrent use; Register is not safe to call concurrently with
// Start or a running Service. Processes that only start runs construct the
// same wiring but simply do not run the Service.
type Engine struct {
	broker       queue.Broker
	store        Store
	scope        func(ctx context.Context) (string, error)
	clk          clock.Clock
	log          *slog.Logger
	workflows    map[string]*registration
	stepAttempts int
}

// NewEngine builds an engine over broker and store. Panics on a nil broker or
// store — the engine is startup wiring, not runtime data.
func NewEngine(broker queue.Broker, store Store, opts ...Option) *Engine {
	if broker == nil {
		panic("workflow: NewEngine requires a broker")
	}
	if store == nil {
		panic("workflow: NewEngine requires a store")
	}
	e := &Engine{
		broker:       broker,
		store:        store,
		clk:          clock.System(),
		log:          logger.NewNope(),
		workflows:    make(map[string]*registration),
		stepAttempts: 5,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Register binds wf to the engine. Panics on a nil workflow or a duplicate
// name — registrations are startup wiring, and failing fast beats two
// definitions silently competing for one queue.
func Register[S any](e *Engine, wf *Workflow[S], opts ...RegisterOption) {
	if wf == nil {
		panic("workflow: Register with a nil workflow")
	}
	if _, dup := e.workflows[wf.name]; dup {
		panic(fmt.Sprintf("workflow: duplicate registration for workflow %q", wf.name))
	}
	total := runAttemptMargin
	for _, st := range wf.steps {
		total += e.stepBudget(st.MaxAttempts)
		if st.Compensate != nil {
			total += e.stepBudget(st.MaxAttempts)
		}
	}
	reg := &registration{def: wf, jobMaxAttempts: total}
	reg.handle = newRunner(e, wf)
	for _, opt := range opts {
		opt(reg)
	}
	e.workflows[wf.name] = reg
}

// stepBudget resolves a step's attempt budget against the engine default.
func (e *Engine) stepBudget(n int) int {
	if n > 0 {
		return n
	}
	return e.stepAttempts
}

// Start creates a run of wf with the given initial state and enqueues its
// driving job; it returns the run id to poll the Store with. The workflow
// must be Registered on this engine (ErrNotRegistered otherwise).
//
// Start is create-then-enqueue, rolled back on failure: a run whose driving
// job cannot be pushed is deleted again before Start returns the push error,
// so a retried Start — including one reusing a WithRunID business key — can
// succeed. In the rare double failure (push and delete both fail) and after
// a process crash between create and push, the run lingers running with no
// driving job; repair via Store.Delete.
func Start[S any](ctx context.Context, e *Engine, wf *Workflow[S], state S, opts ...StartOption) (string, error) {
	reg, ok := e.workflows[wf.Name()]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNotRegistered, wf.Name())
	}
	if reg.def != any(wf) {
		return "", fmt.Errorf("%w: Start(%q) called with a different definition than registered", ErrNotRegistered, wf.Name())
	}
	var cfg startConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	scope := ""
	if e.scope != nil {
		s, err := e.scope(ctx)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return "", ErrScopeMissing
		}
		scope = s
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("workflow: marshal state for %q: %w", wf.Name(), err)
	}
	runID := cfg.runID
	if runID == "" {
		runID = id.NewUUID().String()
	}
	now := e.clk.Now().UTC()
	run := Run{
		ID:        runID,
		Workflow:  wf.Name(),
		Scope:     scope,
		Status:    StatusRunning,
		State:     raw,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := e.store.Create(ctx, run); err != nil {
		return "", fmt.Errorf("workflow: create run for %q: %w", wf.Name(), err)
	}
	payload, err := json.Marshal(runEnvelope{V: wireVersion, RunID: runID})
	if err != nil {
		return "", fmt.Errorf("workflow: encode envelope for %q: %w", wf.Name(), err)
	}
	job := queue.Job{
		ID:          id.NewUUID().String(),
		Queue:       wf.Name(),
		Type:        wf.Name(),
		Payload:     payload,
		Scope:       scope,
		MaxAttempts: reg.jobMaxAttempts,
		RunAt:       now,
		CreatedAt:   now,
	}
	if err := e.broker.Push(ctx, job); err != nil {
		pushErr := fmt.Errorf("workflow: enqueue run %q for %q: %w", runID, wf.Name(), err)
		// Roll back under a non-cancelable context: the push often failed
		// BECAUSE ctx died, and the rollback must still land or the run id
		// stays consumed with no driving job.
		if derr := e.store.Delete(context.WithoutCancel(ctx), runID); derr != nil {
			return "", errors.Join(pushErr, fmt.Errorf("workflow: roll back run %q: %w", runID, derr))
		}
		return "", pushErr
	}
	return runID, nil
}

// NewService builds the worker that executes registered workflows: a
// queue.Service over the engine's broker with one equal-weight queue per
// workflow (a slow workflow only delays itself). Run it under ops/supervisor
// next to any job-queue Service — the default service name is "workflow".
//
// opts pass through to queue.NewService: concurrency, logger, config,
// queue.WithScopeContext for tenancy restore, and a name override all work as
// on a plain queue worker. Do not pass queue.WithQueues — the engine owns the
// queue set, and NewService overrides it.
//
// The service snapshots the engine's registrations at construction: Register
// everything first, then build the service. Returns ErrNoWorkflows when
// nothing was registered.
func NewService(e *Engine, opts ...queue.ServiceOption) (*queue.Service, error) {
	if len(e.workflows) == 0 {
		return nil, ErrNoWorkflows
	}
	weights := make(map[string]int, len(e.workflows))
	for name := range e.workflows {
		weights[name] = 1
	}
	svcOpts := make([]queue.ServiceOption, 0, len(opts)+2)
	svcOpts = append(svcOpts, queue.WithName("workflow"))
	svcOpts = append(svcOpts, opts...)
	svcOpts = append(svcOpts, queue.WithQueues(weights))
	svc, err := queue.NewService(e.broker, svcOpts...)
	if err != nil {
		return nil, err
	}
	for name, reg := range e.workflows {
		queue.Register(svc, queue.NewKind[runEnvelope](name), reg.handle, reg.hopts...)
	}
	return svc, nil
}
