package job

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"sync"
)

// taskExecutor is the internal interface for type-erased task execution.
// This allows storing tasks with different payload types in a single registry.
type taskExecutor interface {
	Execute(ctx context.Context, payload json.RawMessage) error
}

// taskRegistry stores registered task executors by name.
type taskRegistry struct {
	executors map[string]taskExecutor
	mu        sync.RWMutex
}

// newTaskRegistry creates a new task registry.
func newTaskRegistry() *taskRegistry {
	return &taskRegistry{
		executors: make(map[string]taskExecutor),
	}
}

// register adds a task executor to the registry.
func (r *taskRegistry) register(name string, executor taskExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[name] = executor
}

// get retrieves a task executor by name.
func (r *taskRegistry) get(name string) (taskExecutor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	executor, ok := r.executors[name]
	return executor, ok
}

// names returns all registered task names.
func (r *taskRegistry) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Collect(maps.Keys(r.executors))
}

// taskWrapper wraps a typed task handler for type-erased storage.
// It deserializes JSON payloads and calls the typed handler.
type taskWrapper[P any, T interface {
	Name() string
	Handle(context.Context, P) error
}] struct {
	task T
}

// Execute deserializes the payload and calls the typed handler.
func (w *taskWrapper[P, T]) Execute(ctx context.Context, raw json.RawMessage) error {
	var payload P
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return errors.Join(ErrInvalidPayload, err)
		}
	}
	return w.task.Handle(ctx, payload)
}

// newTaskWrapper creates a new type-erased wrapper for a typed task.
func newTaskWrapper[P any, T interface {
	Name() string
	Handle(context.Context, P) error
}](task T) *taskWrapper[P, T] {
	return &taskWrapper[P, T]{task: task}
}
