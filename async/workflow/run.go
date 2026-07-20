package workflow

import (
	"context"
	"encoding/json"
	"time"
)

// Status is a run's lifecycle state.
type Status string

const (
	// StatusRunning means forward steps are executing (or awaiting retry).
	StatusRunning Status = "running"
	// StatusCompensating means a permanent failure occurred and completed
	// steps' compensations are unwinding in reverse order.
	StatusCompensating Status = "compensating"
	// StatusCompleted means every step finished. Terminal.
	StatusCompleted Status = "completed"
	// StatusFailed means the run failed permanently and compensation (if any
	// was defined) finished. The triggering error is on Run.Error. Terminal.
	StatusFailed Status = "failed"
)

// Run is one workflow execution's checkpoint. While running, Step is the
// index of the next step to execute and Attempt counts that step's failed
// attempts so far; while compensating, Step walks backwards over the steps
// whose compensations still have to run. State is the JSON-encoded workflow
// state as of the last checkpoint. Error records the permanent failure that
// triggered compensation — or, on a non-terminal run whose driving job was
// dead-lettered over unprocessable state, the reason it was abandoned.
// Version implements optimistic locking — see Store.
type Run struct {
	CreatedAt time.Time
	UpdatedAt time.Time
	ID        string
	Workflow  string
	Scope     string
	Status    Status
	Error     string
	State     json.RawMessage
	Step      int
	Attempt   int
	Version   int
}

// Store persists run checkpoints. Implementations must be safe for concurrent
// use. MemoryStore is the in-process test double; async/workflow/postgres
// ships the durable implementation.
//
// Update is guarded by optimistic locking: it persists run only when the
// stored version equals run.Version, incrementing the stored version by one;
// a mismatch returns ErrStaleRun and leaves the row untouched. That stops a
// worker whose queue lease was silently lost from regressing a checkpoint the
// new owner already advanced.
type Store interface {
	// Create persists a new run; ErrRunAlreadyExists when the id is taken.
	Create(ctx context.Context, run Run) error
	// Get returns the run by id; ErrRunNotFound when absent.
	Get(ctx context.Context, id string) (Run, error)
	// Update persists run if the stored version equals run.Version (see
	// above); ErrRunNotFound when absent, ErrStaleRun on a version mismatch.
	Update(ctx context.Context, run Run) error
	// Delete removes a run; ErrRunNotFound when absent. Start uses it to roll
	// back a run whose driving job could not be enqueued; consumers may use
	// it to repair orphans.
	Delete(ctx context.Context, id string) error
}
