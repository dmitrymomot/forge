package fsm

import (
	"fmt"
	"strings"
)

// Define is a zero-size type-parameter anchor: declare
// `var d fsm.Define[Status, *Task]` once and every part constructed
// through d infers both type parameters. It is stateless — parts still
// flow into New.
type Define[S ~string, V any] struct{}

// Attachment is a guard or hook bound to an edge or state via Edge,
// EdgeFromAny, OnEnter, or OnExit.
type Attachment[S ~string, V any] struct {
	fn      Func[S, V]
	isGuard bool
}

type partKind uint8

const (
	partEdge partKind = iota
	partOnEnter
	partOnExit
	partEdgeFromAny
	partState
)

// Part is one construction element consumed by New.
type Part[S ~string, V any] struct {
	from   S
	to     S
	guards []Func[S, V]
	hooks  []Func[S, V]
	kind   partKind
}

func split[S ~string, V any](atts []Attachment[S, V]) (guards, hooks []Func[S, V]) {
	for _, a := range atts {
		if a.isGuard {
			guards = append(guards, a.fn)
		} else {
			hooks = append(hooks, a.fn)
		}
	}
	return guards, hooks
}

// Edge declares a transition from -> to with optional guard/hook
// attachments. A self-transition requires an explicit Edge(s, s).
func (Define[S, V]) Edge(from, to S, atts ...Attachment[S, V]) Part[S, V] {
	guards, hooks := split(atts)
	return Part[S, V]{kind: partEdge, from: from, to: to, guards: guards, hooks: hooks}
}

// EdgeFromAny declares a wildcard edge into to from every other declared
// state. It expands at construction into concrete edges — pure sugar, no
// runtime wildcard logic. An explicit Edge for a pair fully replaces the
// expanded one; no self-loop is generated (a self-transition needs an
// explicit Edge(s, s)).
func (Define[S, V]) EdgeFromAny(to S, atts ...Attachment[S, V]) Part[S, V] {
	guards, hooks := split(atts)
	return Part[S, V]{kind: partEdgeFromAny, to: to, guards: guards, hooks: hooks}
}

// declareState declares a state without edges or attachments. Compile uses
// it so every explicitly listed StateDef exists in the machine even when
// nothing references it.
func declareState[S ~string, V any](name S) Part[S, V] {
	return Part[S, V]{kind: partState, from: name}
}

// Guard wraps fn as a guard attachment: a side-effect-free check over
// preloaded data on v that denies the transition by returning an error.
// Keep I/O out of guards so ErrGuardDenied always means a domain denial.
func (Define[S, V]) Guard(fn Func[S, V]) Attachment[S, V] {
	return Attachment[S, V]{fn: fn, isGuard: true}
}

// Hook wraps fn as a hook attachment: it may mutate v; an error aborts the
// transition. Hooks run only after every guard passed and must confine
// themselves to entity mutation — external effects belong after the
// caller persists.
func (Define[S, V]) Hook(fn Func[S, V]) Attachment[S, V] {
	return Attachment[S, V]{fn: fn}
}

// OnEnter attaches guards/hooks that run on every transition into state,
// regardless of source edge. Multiple OnEnter parts for one state append
// in declaration order.
func (Define[S, V]) OnEnter(state S, atts ...Attachment[S, V]) Part[S, V] {
	guards, hooks := split(atts)
	return Part[S, V]{kind: partOnEnter, from: state, guards: guards, hooks: hooks}
}

// OnExit attaches guards/hooks that run on every transition out of state,
// regardless of target edge. Multiple OnExit parts for one state append
// in declaration order.
func (Define[S, V]) OnExit(state S, atts ...Attachment[S, V]) Part[S, V] {
	guards, hooks := split(atts)
	return Part[S, V]{kind: partOnExit, from: state, guards: guards, hooks: hooks}
}

// New compiles a typed machine. States are inferred: every state mentioned
// by initial or any part is declared, in first-mention order. All
// validation issues are aggregated into one single-line
// ErrInvalidDefinition.
func New[S ~string, V any](initial S, parts ...Part[S, V]) (*Machine[S, V], error) {
	return build(initial, parts, nil)
}

// MustNew is New that panics on error, for var initialization of
// compile-time flows (the regexp.MustCompile precedent).
func MustNew[S ~string, V any](initial S, parts ...Part[S, V]) *Machine[S, V] {
	m, err := New(initial, parts...)
	if err != nil {
		panic(err)
	}
	return m
}

type builder[S ~string, V any] struct {
	stateSet  map[S]struct{}
	edges     map[edgeKey[S]]callbacks[S, V]
	enter     map[S]callbacks[S, V]
	exit      map[S]callbacks[S, V]
	issues    []string
	states    []S
	nextOrder []edgeKey[S]
}

func (b *builder[S, V]) declare(name S) {
	if _, ok := b.stateSet[name]; ok {
		return
	}
	if issue := checkName(string(name)); issue != "" {
		b.issues = append(b.issues, issue)
	}
	b.stateSet[name] = struct{}{}
	b.states = append(b.states, name)
}

func checkName(name string) string {
	switch {
	case name == "":
		return "empty state name"
	case name == "*":
		return `state named "*"`
	case strings.TrimSpace(name) != name:
		return fmt.Sprintf("state name %q has leading or trailing whitespace", name)
	}
	return ""
}

// build is the shared constructor behind New and Compile. preIssues lets
// Compile merge its definition-level issues into the same single-line
// aggregate.
func build[S ~string, V any](initial S, parts []Part[S, V], preIssues []string) (*Machine[S, V], error) {
	b := &builder[S, V]{
		issues:   preIssues,
		stateSet: make(map[S]struct{}),
		edges:    make(map[edgeKey[S]]callbacks[S, V]),
		enter:    make(map[S]callbacks[S, V]),
		exit:     make(map[S]callbacks[S, V]),
	}

	// Pass 1: declare every mentioned state in first-mention order.
	b.declare(initial)
	for _, p := range parts {
		switch p.kind {
		case partEdge:
			b.declare(p.from)
			b.declare(p.to)
		case partOnEnter, partOnExit, partState:
			b.declare(p.from)
		case partEdgeFromAny:
			b.declare(p.to)
		}
	}

	// Pass 2: duplicate detection over literal (from, to) pairs.
	explicit := make(map[edgeKey[S]]struct{}, len(parts))
	wildcardSeen := make(map[S]struct{})
	for _, p := range parts {
		switch p.kind {
		case partEdge:
			key := edgeKey[S]{from: p.from, to: p.to}
			if _, dup := explicit[key]; dup {
				b.issues = append(b.issues, fmt.Sprintf("duplicate edge %s -> %s", p.from, p.to))
				continue
			}
			explicit[key] = struct{}{}
		case partEdgeFromAny:
			if _, dup := wildcardSeen[p.to]; dup {
				b.issues = append(b.issues, fmt.Sprintf("duplicate edge * -> %s", p.to))
				continue
			}
			wildcardSeen[p.to] = struct{}{}
		case partOnEnter, partOnExit, partState:
		}
	}

	// Pass 3: materialize edges in declaration order.
	wildcardDone := make(map[S]struct{})
	for _, p := range parts {
		switch p.kind {
		case partEdge:
			key := edgeKey[S]{from: p.from, to: p.to}
			if _, exists := b.edges[key]; exists {
				continue // duplicate, reported in pass 2
			}
			b.edges[key] = callbacks[S, V]{guards: p.guards, hooks: p.hooks}
			b.nextOrder = append(b.nextOrder, key)
		case partEdgeFromAny:
			if _, dup := wildcardDone[p.to]; dup {
				continue // duplicate, reported in pass 2
			}
			wildcardDone[p.to] = struct{}{}
			for _, from := range b.states {
				if from == p.to {
					continue // wildcards never generate self-loops
				}
				key := edgeKey[S]{from: from, to: p.to}
				if _, isExplicit := explicit[key]; isExplicit {
					continue // an explicit edge fully replaces the expanded one
				}
				b.edges[key] = callbacks[S, V]{guards: p.guards, hooks: p.hooks}
				b.nextOrder = append(b.nextOrder, key)
			}
		case partOnEnter:
			cbs := b.enter[p.from]
			cbs.guards = append(cbs.guards, p.guards...)
			cbs.hooks = append(cbs.hooks, p.hooks...)
			b.enter[p.from] = cbs
		case partOnExit:
			cbs := b.exit[p.from]
			cbs.guards = append(cbs.guards, p.guards...)
			cbs.hooks = append(cbs.hooks, p.hooks...)
			b.exit[p.from] = cbs
		case partState:
		}
	}

	if len(b.issues) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDefinition, strings.Join(b.issues, "; "))
	}

	next := make(map[S][]S, len(b.states))
	for _, key := range b.nextOrder {
		next[key.from] = append(next[key.from], key.to)
	}
	return &Machine[S, V]{
		stateSet: b.stateSet,
		edges:    b.edges,
		enter:    b.enter,
		exit:     b.exit,
		next:     next,
		states:   b.states,
		initial:  initial,
	}, nil
}
