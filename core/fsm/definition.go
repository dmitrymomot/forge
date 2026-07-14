package fsm

import "fmt"

// Definition is the serializable, tenant-facing flow description: states
// and edges by name, guards and hooks referenced by registry name.
// Consumers store it (typically JSON in their own DB) and compile it
// against their registered vocabulary. Definitions arrive from the
// consumer's own flow-save API — that API is where size limits belong;
// the package imposes no hidden caps.
type Definition struct {
	Initial string     `json:"initial"`
	States  []StateDef `json:"states"`
	Edges   []EdgeDef  `json:"edges"`
}

// StateDef declares one state and its state-level guard/hook names.
type StateDef struct {
	Name          string   `json:"name"`
	OnEnterGuards []string `json:"on_enter_guards,omitempty"`
	OnEnterHooks  []string `json:"on_enter_hooks,omitempty"`
	OnExitGuards  []string `json:"on_exit_guards,omitempty"`
	OnExitHooks   []string `json:"on_exit_hooks,omitempty"`
}

// EdgeDef declares one edge; From may be "*", the source-side wildcard
// (an edge from every other declared state).
type EdgeDef struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Guards []string `json:"guards,omitempty"`
	Hooks  []string `json:"hooks,omitempty"`
}

// Registry is the guard/hook vocabulary an application exposes to its
// flow definitions. Nil maps are valid for definitions that reference no
// names.
type Registry[V any] struct {
	Guards map[string]Func[string, V]
	Hooks  map[string]Func[string, V]
}

// Compile validates def, resolves its guard/hook names against reg, and
// builds the machine. All issues are aggregated into one single-line
// ErrInvalidDefinition so a flow-builder shows every problem in one save
// attempt. A definition that compiles fires cleanly — Fire never reports
// a definition problem.
func Compile[V any](def Definition, reg Registry[V]) (*Machine[string, V], error) {
	if len(def.States) == 0 {
		return nil, fmt.Errorf("%w: empty state set", ErrInvalidDefinition)
	}

	var issues []string
	declared := make(map[string]struct{}, len(def.States))
	for _, s := range def.States {
		if _, dup := declared[s.Name]; dup {
			issues = append(issues, fmt.Sprintf("duplicate state %q", s.Name))
			continue
		}
		declared[s.Name] = struct{}{}
	}
	if _, ok := declared[def.Initial]; !ok {
		issues = append(issues, fmt.Sprintf("initial state %q not declared", def.Initial))
	}
	for _, e := range def.Edges {
		if e.From != "*" {
			if _, ok := declared[e.From]; !ok {
				issues = append(issues, fmt.Sprintf("edge references undeclared state %q", e.From))
			}
		}
		if _, ok := declared[e.To]; !ok {
			issues = append(issues, fmt.Sprintf("edge references undeclared state %q", e.To))
		}
	}

	resolve := func(names []string, fns map[string]Func[string, V], kind string) []Func[string, V] {
		out := make([]Func[string, V], 0, len(names))
		for _, name := range names {
			fn, ok := fns[name]
			if !ok {
				issues = append(issues, fmt.Sprintf("unknown %s %q", kind, name))
				continue
			}
			out = append(out, fn)
		}
		return out
	}

	var d Define[string, V]
	toAtts := func(guards, hooks []Func[string, V]) []Attachment[string, V] {
		atts := make([]Attachment[string, V], 0, len(guards)+len(hooks))
		for _, g := range guards {
			atts = append(atts, d.Guard(g))
		}
		for _, h := range hooks {
			atts = append(atts, d.Hook(h))
		}
		return atts
	}

	parts := make([]Part[string, V], 0, 2*len(def.States)+len(def.Edges))
	for _, s := range def.States {
		parts = append(parts, declareState[string, V](s.Name))
		if atts := toAtts(resolve(s.OnEnterGuards, reg.Guards, "guard"), resolve(s.OnEnterHooks, reg.Hooks, "hook")); len(atts) > 0 {
			parts = append(parts, d.OnEnter(s.Name, atts...))
		}
		if atts := toAtts(resolve(s.OnExitGuards, reg.Guards, "guard"), resolve(s.OnExitHooks, reg.Hooks, "hook")); len(atts) > 0 {
			parts = append(parts, d.OnExit(s.Name, atts...))
		}
	}
	for _, e := range def.Edges {
		atts := toAtts(resolve(e.Guards, reg.Guards, "guard"), resolve(e.Hooks, reg.Hooks, "hook"))
		if e.From == "*" {
			parts = append(parts, d.EdgeFromAny(e.To, atts...))
		} else {
			parts = append(parts, d.Edge(e.From, e.To, atts...))
		}
	}

	return build(def.Initial, parts, issues)
}
