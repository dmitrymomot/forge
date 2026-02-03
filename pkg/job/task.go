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
// Type erasure via json.RawMessage enables compile-time type safety
// (Handle methods validate payload types) while allowing heterogeneous storage.
type taskExecutor interface {
	Execute(ctx context.Context, payload json.RawMessage) error
}

type taskRegistry struct {
	executors map[string]taskExecutor
	mu        sync.RWMutex
}

func newTaskRegistry() *taskRegistry {
	return &taskRegistry{
		executors: make(map[string]taskExecutor),
	}
}

func (r *taskRegistry) register(name string, executor taskExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[name] = executor
}

func (r *taskRegistry) get(name string) (taskExecutor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	executor, ok := r.executors[name]
	return executor, ok
}

func (r *taskRegistry) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Collect(maps.Keys(r.executors))
}

// taskWrapper implements taskExecutor by deserializing JSON payloads
// and calling the typed Handler method. This provides compile-time safety
// without exposing type parameters to the registry.
type taskWrapper[P any, T interface {
	Name() string
	Handle(context.Context, P) error
}] struct {
	task T
}

func (w *taskWrapper[P, T]) Execute(ctx context.Context, raw json.RawMessage) error {
	var payload P
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return errors.Join(ErrInvalidPayload, err)
		}
	}
	return w.task.Handle(ctx, payload)
}

func newTaskWrapper[P any, T interface {
	Name() string
	Handle(context.Context, P) error
}](task T) *taskWrapper[P, T] {
	return &taskWrapper[P, T]{task: task}
}
