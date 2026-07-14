package fsm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/dmitrymomot/forge/core/fsm"
)

func BenchmarkFire_BareEdge(b *testing.B) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen, d.Edge(statusOpen, statusDone))
	v := &task{}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := m.Fire(ctx, v, statusOpen, statusDone); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFire_GuardsAndHooks(b *testing.B) {
	pass := func(ctx context.Context, v *task, from, to status) error { return nil }
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusDone, d.Guard(pass), d.Hook(pass)),
		d.OnEnter(statusDone, d.Guard(pass), d.Hook(pass)),
		d.OnExit(statusOpen, d.Guard(pass), d.Hook(pass)),
	)
	v := &task{}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := m.Fire(ctx, v, statusOpen, statusDone); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAllowed(b *testing.B) {
	deny := func(ctx context.Context, v *task, from, to status) error { return errors.New("no") }
	pass := func(ctx context.Context, v *task, from, to status) error { return nil }
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusInProgress, d.Guard(pass)),
		d.Edge(statusOpen, statusReview, d.Guard(deny)),
		d.Edge(statusOpen, statusDone, d.Guard(pass)),
		d.EdgeFromAny(statusCancelled),
	)
	v := &task{}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Allowed(ctx, v, statusOpen); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompile(b *testing.B) {
	// Realistic tenant flow: 10 states, ~25 edges, a wildcard, named callbacks.
	states := make([]fsm.StateDef, 10)
	for i := range 10 {
		states[i] = fsm.StateDef{Name: fmt.Sprintf("state_%d", i)}
	}
	states[9].OnEnterGuards = []string{"check"}
	states[9].OnEnterHooks = []string{"stamp"}
	edges := make([]fsm.EdgeDef, 0, 25)
	for i := range 8 {
		edges = append(edges,
			fsm.EdgeDef{From: fmt.Sprintf("state_%d", i), To: fmt.Sprintf("state_%d", i+1)},
			fsm.EdgeDef{From: fmt.Sprintf("state_%d", i+1), To: fmt.Sprintf("state_%d", i)},
			fsm.EdgeDef{From: fmt.Sprintf("state_%d", i), To: "state_9", Guards: []string{"check"}},
		)
	}
	edges = append(edges, fsm.EdgeDef{From: "*", To: "state_0"})
	def := fsm.Definition{Initial: "state_0", States: states, Edges: edges}
	reg := fsm.Registry[*task]{
		Guards: map[string]fsm.Func[string, *task]{
			"check": func(ctx context.Context, v *task, from, to string) error { return nil },
		},
		Hooks: map[string]fsm.Func[string, *task]{
			"stamp": func(ctx context.Context, v *task, from, to string) error { return nil },
		},
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fsm.Compile(def, reg); err != nil {
			b.Fatal(err)
		}
	}
}
