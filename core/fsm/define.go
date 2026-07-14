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
		if p.kind == partEdge {
			b.declare(p.from)
			b.declare(p.to)
		}
	}

	// Pass 2: duplicate detection over literal (from, to) pairs.
	explicit := make(map[edgeKey[S]]struct{}, len(parts))
	for _, p := range parts {
		if p.kind == partEdge {
			key := edgeKey[S]{from: p.from, to: p.to}
			if _, dup := explicit[key]; dup {
				b.issues = append(b.issues, fmt.Sprintf("duplicate edge %s -> %s", p.from, p.to))
				continue
			}
			explicit[key] = struct{}{}
		}
	}

	// Pass 3: materialize edges in declaration order.
	for _, p := range parts {
		if p.kind == partEdge {
			key := edgeKey[S]{from: p.from, to: p.to}
			if _, exists := b.edges[key]; exists {
				continue // duplicate, reported in pass 2
			}
			b.edges[key] = callbacks[S, V]{guards: p.guards, hooks: p.hooks}
			b.nextOrder = append(b.nextOrder, key)
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
