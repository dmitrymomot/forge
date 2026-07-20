package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dmitrymomot/forge/async/queue"
)

// newRunner builds the queue handler driving one workflow's runs. Every
// invocation loads the freshest checkpoint, executes as far as it can —
// checkpointing after each step — and reports a queue verdict: nil when the
// run reached a terminal status or made its checkpointed progress, an error
// to retry (transient step failure, store hiccup, shutdown mid-step), or
// queue.SkipRetry for poison input and exhausted compensations.
func newRunner[S any](e *Engine, wf *Workflow[S]) func(ctx context.Context, env runEnvelope) error {
	return func(ctx context.Context, env runEnvelope) error {
		if env.V != wireVersion {
			return queue.SkipRetry(fmt.Errorf("workflow: %q: unsupported envelope version %d", wf.name, env.V))
		}
		if env.RunID == "" {
			return queue.SkipRetry(fmt.Errorf("workflow: %q: envelope without run id", wf.name))
		}
		run, err := e.store.Get(ctx, env.RunID)
		if err != nil {
			if errors.Is(err, ErrRunNotFound) {
				return queue.SkipRetry(fmt.Errorf("workflow: %q: run %q not found", wf.name, env.RunID))
			}
			return fmt.Errorf("workflow: load run %q: %w", env.RunID, err)
		}
		if run.Workflow != wf.name {
			return queue.SkipRetry(fmt.Errorf("workflow: run %q belongs to workflow %q, not %q", run.ID, run.Workflow, wf.name))
		}
		if run.Status == StatusCompleted || run.Status == StatusFailed {
			return nil // duplicate delivery of a finished run
		}
		state, err := decodeState[S](run.State)
		if err != nil {
			return e.abandon(ctx, &run, fmt.Errorf("workflow: run %q: decode state: %w", run.ID, err))
		}
		if run.Status == StatusRunning {
			if err := forward(ctx, e, wf, &run, state); err != nil {
				return err
			}
			if run.Status != StatusCompensating {
				return nil
			}
			// The failing step may have half-mutated state before erroring;
			// compensations must see the last checkpoint, not those leftovers.
			state, err = decodeState[S](run.State)
			if err != nil {
				return e.abandon(ctx, &run, fmt.Errorf("workflow: run %q: decode state: %w", run.ID, err))
			}
		}
		return compensate(ctx, e, wf, &run, state)
	}
}

func decodeState[S any](raw json.RawMessage) (*S, error) {
	state := new(S)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, state); err != nil {
			return nil, err
		}
	}
	return state, nil
}

// forward executes steps from the checkpoint until the run completes, a
// transient failure returns an error for the engine to retry, or a permanent
// failure transitions the run to StatusCompensating (or straight to
// StatusFailed when no completed step has a compensation) and returns nil for
// the caller to continue.
func forward[S any](ctx context.Context, e *Engine, wf *Workflow[S], run *Run, state *S) error {
	steps := wf.steps
	if run.Step < 0 {
		return queue.SkipRetry(fmt.Errorf("workflow: run %q: inconsistent checkpoint step %d", run.ID, run.Step))
	}
	for run.Step < len(steps) {
		if err := ctx.Err(); err != nil {
			return err // shutdown or timeout between steps: resume on redelivery
		}
		st := steps[run.Step]
		err := invokeStep(ctx, st.Run, state)
		if err == nil {
			raw, merr := json.Marshal(state)
			if merr != nil {
				return e.abandon(ctx, run, fmt.Errorf("workflow: run %q: marshal state after step %q: %w", run.ID, st.Name, merr))
			}
			run.State = raw
			run.Step++
			run.Attempt = 0
			run.Error = "" // a resumed abandoned run is making progress again
			if run.Step == len(steps) {
				run.Status = StatusCompleted
			}
			if cerr := e.checkpoint(ctx, run); cerr != nil {
				return cerr
			}
			continue
		}
		// Permanent verdicts outrank cancellation: letting queue.Cancel or
		// queue.SkipRetry through raw would ack or dead-letter the driving job
		// while the run still looks alive. A plain error with the handler ctx
		// cancelled means the lease was lost mid-step — the new claim owns the
		// run now, so leave the checkpoint alone; a deadline expiry is the
		// step's own timeout and burns an attempt like any other failure.
		if !isPermanent(err) && errors.Is(ctx.Err(), context.Canceled) {
			return err
		}
		failed := run.Attempt + 1
		if !isPermanent(err) && failed < e.stepBudget(st.MaxAttempts) {
			run.Attempt = failed
			if cerr := e.checkpoint(ctx, run); cerr != nil {
				return cerr
			}
			return fmt.Errorf("workflow: run %q step %q attempt %d: %w", run.ID, st.Name, failed, err)
		}
		run.Error = err.Error()
		run.Attempt = 0
		run.Step = prevCompIndex(steps, run.Step-1)
		if run.Step < 0 {
			run.Status = StatusFailed
		} else {
			run.Status = StatusCompensating
		}
		if cerr := e.checkpoint(ctx, run); cerr != nil {
			return cerr
		}
		if run.Status == StatusFailed {
			e.log.ErrorContext(ctx, "workflow run failed", runAttrs(run, slog.String("step", st.Name), slog.String("error", run.Error))...)
		}
		return nil
	}
	// Definition drift across deploys can leave a checkpoint past the last
	// step; a run with nothing left to do is complete.
	if run.Status != StatusCompleted {
		run.Status = StatusCompleted
		if cerr := e.checkpoint(ctx, run); cerr != nil {
			return cerr
		}
	}
	e.log.InfoContext(ctx, "workflow run completed", runAttrs(run)...)
	return nil
}

// compensate unwinds completed steps' compensations newest first from the
// checkpoint, ending in StatusFailed. A compensation that exhausts its budget
// keeps the run compensating, resets its attempt counter, and dead-letters
// the driving job — queue.Client.Requeue resumes the unwind.
func compensate[S any](ctx context.Context, e *Engine, wf *Workflow[S], run *Run, state *S) error {
	steps := wf.steps
	for run.Status == StatusCompensating {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Recompute defensively: the checkpoint normally indexes a step with a
		// compensation, but definition drift across deploys can invalidate it.
		run.Step = prevCompIndex(steps, min(run.Step, len(steps)-1))
		if run.Step < 0 {
			run.Status = StatusFailed
			if cerr := e.checkpoint(ctx, run); cerr != nil {
				return cerr
			}
			break
		}
		st := steps[run.Step]
		err := invokeStep(ctx, st.Compensate, state)
		if err == nil {
			raw, merr := json.Marshal(state)
			if merr != nil {
				return e.abandon(ctx, run, fmt.Errorf("workflow: run %q: marshal state after compensation %q: %w", run.ID, st.Name, merr))
			}
			run.State = raw
			run.Attempt = 0
			run.Step = prevCompIndex(steps, run.Step-1)
			if run.Step < 0 {
				run.Status = StatusFailed
			}
			if cerr := e.checkpoint(ctx, run); cerr != nil {
				return cerr
			}
			continue
		}
		// Same classification as forward: permanent verdicts outrank
		// cancellation, lease loss (Canceled) yields without burning an
		// attempt, a deadline expiry burns one.
		if !isPermanent(err) && errors.Is(ctx.Err(), context.Canceled) {
			return err
		}
		failed := run.Attempt + 1
		if !isPermanent(err) && failed < e.stepBudget(st.MaxAttempts) {
			run.Attempt = failed
			if cerr := e.checkpoint(ctx, run); cerr != nil {
				return cerr
			}
			return fmt.Errorf("workflow: run %q compensation %q attempt %d: %w", run.ID, st.Name, failed, err)
		}
		// Exhausted: reset the counter so a Requeue retries this compensation
		// with a fresh budget instead of dead-lettering again immediately.
		run.Attempt = 0
		if cerr := e.checkpoint(ctx, run); cerr != nil {
			return cerr
		}
		e.log.ErrorContext(ctx, "workflow compensation exhausted, run dead-lettered", runAttrs(run, slog.String("step", st.Name), slog.String("error", err.Error()))...)
		return queue.SkipRetry(fmt.Errorf("workflow: run %q compensation %q exhausted: %w", run.ID, st.Name, err))
	}
	e.log.ErrorContext(ctx, "workflow run failed after compensation", runAttrs(run, slog.String("error", run.Error))...)
	return nil
}

// checkpoint persists run and tracks the version bump locally. It writes
// under a non-cancelable context: a state transition that was decided must
// commit even when the handler ctx died mid-step (timeout expiry racing a
// permanent verdict), mirroring the queue engine's post-claim broker ops. A
// worker that lost its lease is stopped by the version guard instead.
func (e *Engine) checkpoint(ctx context.Context, run *Run) error {
	run.UpdatedAt = e.clk.Now().UTC()
	if err := e.store.Update(context.WithoutCancel(ctx), *run); err != nil {
		return fmt.Errorf("workflow: checkpoint run %q: %w", run.ID, err)
	}
	run.Version++
	return nil
}

// abandon dead-letters the driving job over unprocessable input (state that
// no longer decodes or marshals — schema drift of S across a deploy),
// recording the reason on Run.Error so the store stays diagnosable. The
// status is left as is: after a fixed deploy, queue.Client.Requeue resumes
// the run from its checkpoint. The write is best-effort — the DLQ entry
// carries the same reason.
func (e *Engine) abandon(ctx context.Context, run *Run, err error) error {
	// A compensating run's Error holds the business failure that triggered
	// the unwind — the one signal explaining a stuck saga — so append the
	// abandon note instead of replacing it. The suffix check keeps repeated
	// requeue-and-abandon cycles from growing the note unboundedly.
	note := "abandoned: " + err.Error()
	switch {
	case run.Error == "":
		run.Error = note
	case !strings.HasSuffix(run.Error, note):
		run.Error += "; " + note
	}
	if cerr := e.checkpoint(ctx, run); cerr != nil {
		e.log.WarnContext(ctx, "workflow abandon note not persisted", runAttrs(run, slog.Any("error", cerr))...)
	}
	e.log.ErrorContext(ctx, "workflow run abandoned, driving job dead-lettered", runAttrs(run, slog.String("error", run.Error))...)
	return queue.SkipRetry(err)
}

// invokeStep runs one step (or compensation) with panic recovery: a panicking
// step is a failed step, not a crashed worker, so it burns an attempt like
// any other failure.
func invokeStep[S any](ctx context.Context, fn func(context.Context, *S) error, state *S) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("workflow: step panic: %v", r)
		}
	}()
	return fn(ctx, state)
}

// prevCompIndex returns the highest index <= from whose step has a
// compensation, or -1 when none is left to run.
func prevCompIndex[S any](steps []Step[S], from int) int {
	for i := min(from, len(steps)-1); i >= 0; i-- {
		if steps[i].Compensate != nil {
			return i
		}
	}
	return -1
}

// isPermanent classifies a step error: the Fail verdict, plus the queue
// verdicts a handler author might return by habit — letting either leak to
// the queue engine would dead-letter or discard the driving job while the run
// still looks alive.
func isPermanent(err error) bool {
	return IsFail(err) || queue.IsSkipRetry(err) || errors.Is(err, queue.Cancel)
}

func runAttrs(run *Run, extra ...any) []any {
	attrs := make([]any, 0, 2+len(extra))
	attrs = append(attrs, slog.String("workflow", run.Workflow), slog.String("run_id", run.ID))
	return append(attrs, extra...)
}
