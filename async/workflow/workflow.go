package workflow

import (
	"context"
	"fmt"
	"slices"
)

// Step is one named unit of a workflow over state S. Run executes forward
// work and may mutate state; the mutation is checkpointed only when Run
// returns nil. Compensate, when set, undoes Run after a later step fails
// permanently — it sees the state as of the last checkpoint (a failed step's
// partial mutations are discarded) and its own mutations are checkpointed
// too, so an undo can record what it released. MaxAttempts bounds the step's
// transient failures before the failure turns permanent (0 uses the engine's
// WithStepAttempts default); the same budget bounds Compensate.
//
// Both funcs run under the queue engine's at-least-once delivery and MUST BE
// IDEMPOTENT.
type Step[S any] struct {
	Run         func(ctx context.Context, state *S) error
	Compensate  func(ctx context.Context, state *S) error
	Name        string
	MaxAttempts int
}

// Workflow is an immutable, ordered step sequence over state S. Declare one
// package-level Workflow per sequence with New and share it between the
// process that Starts runs and the worker that executes them.
type Workflow[S any] struct {
	name  string
	steps []Step[S]
}

// New declares a workflow. The name must be non-empty and unique across the
// application (convention: "domain.action"); it becomes the queue the
// workflow's runs drain from. Panics on an empty or duplicate step name, a
// nil Step.Run, a negative Step.MaxAttempts, or no steps at all — workflows
// are startup wiring, not runtime data.
func New[S any](name string, steps ...Step[S]) *Workflow[S] {
	if name == "" {
		panic("workflow: New requires a non-empty workflow name")
	}
	if len(steps) == 0 {
		panic(fmt.Sprintf("workflow: New(%q) requires at least one step", name))
	}
	seen := make(map[string]struct{}, len(steps))
	for i, st := range steps {
		if st.Name == "" {
			panic(fmt.Sprintf("workflow: New(%q) step %d has an empty name", name, i))
		}
		if st.Run == nil {
			panic(fmt.Sprintf("workflow: New(%q) step %q has a nil Run", name, st.Name))
		}
		if st.MaxAttempts < 0 {
			panic(fmt.Sprintf("workflow: New(%q) step %q has negative MaxAttempts", name, st.Name))
		}
		if _, dup := seen[st.Name]; dup {
			panic(fmt.Sprintf("workflow: New(%q) duplicate step name %q", name, st.Name))
		}
		seen[st.Name] = struct{}{}
	}
	return &Workflow[S]{name: name, steps: slices.Clone(steps)}
}

// Name returns the workflow name.
func (w *Workflow[S]) Name() string { return w.name }
