package fsm

import (
	"context"
	"fmt"
)

// Func is the single callback type for guards and hooks. Guards are
// side-effect-free checks over preloaded data on v; hooks may mutate v.
// Either aborts the transition by returning an error.
type Func[S ~string, V any] func(ctx context.Context, v V, from, to S) error

type edgeKey[S ~string] struct{ from, to S }

type callbacks[S ~string, V any] struct {
	guards []Func[S, V]
	hooks  []Func[S, V]
}

// Machine is an immutable compiled transition table. It holds no instance
// state — the current state lives in the caller's storage and is passed
// into every call — so one machine is safely shared across goroutines.
type Machine[S ~string, V any] struct {
	initial  S
	stateSet map[S]struct{}
	edges    map[edgeKey[S]]callbacks[S, V]
	enter    map[S]callbacks[S, V]
	exit     map[S]callbacks[S, V]
	next     map[S][]S
	states   []S
}

// Fire validates and executes the transition from -> to for v: legality,
// then guards (exit, edge, enter), then hooks (exit, edge, enter), each
// list in declaration order; the first error aborts. A nil return means
// approved — the caller persists the new state. On error nothing was
// persisted; the caller discards the loaded v.
func (m *Machine[S, V]) Fire(ctx context.Context, v V, from, to S) error {
	if _, ok := m.stateSet[from]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	if _, ok := m.stateSet[to]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownState, to)
	}
	edge, ok := m.edges[edgeKey[S]{from: from, to: to}]
	if !ok {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, from, to)
	}
	exit, enter := m.exit[from], m.enter[to]
	for _, guards := range [3][]Func[S, V]{exit.guards, edge.guards, enter.guards} {
		for _, guard := range guards {
			if err := guard(ctx, v, from, to); err != nil {
				return fmt.Errorf("%w: %s -> %s: %w", ErrGuardDenied, from, to, err)
			}
		}
	}
	for _, hooks := range [3][]Func[S, V]{exit.hooks, edge.hooks, enter.hooks} {
		for _, hook := range hooks {
			if err := hook(ctx, v, from, to); err != nil {
				return fmt.Errorf("%w: %s -> %s: %w", ErrHookFailed, from, to, err)
			}
		}
	}
	return nil
}

// Can reports whether an edge from -> to exists. It ignores guards.
func (m *Machine[S, V]) Can(from, to S) bool {
	_, ok := m.edges[edgeKey[S]{from: from, to: to}]
	return ok
}

// Next returns the structural targets reachable from the given state in
// declaration order, nil when the state is unknown or has no outgoing
// edges. The returned slice is an independent copy.
func (m *Machine[S, V]) Next(from S) []S {
	targets := m.next[from]
	if len(targets) == 0 {
		return nil
	}
	out := make([]S, len(targets))
	copy(out, targets)
	return out
}

// Initial returns the machine's declared initial state.
func (m *Machine[S, V]) Initial() S { return m.initial }

// States returns all declared states in first-mention order. The returned
// slice is an independent copy.
func (m *Machine[S, V]) States() []S {
	out := make([]S, len(m.states))
	copy(out, m.states)
	return out
}
