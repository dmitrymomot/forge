package smartlink

import "slices"

// Decider makes the per-click decision. *Compiled implements it; decorators
// (fraud guards, metrics, A/B overrides) wrap a Decider to build another one.
type Decider interface {
	Decide(Visit) Decision
}

// DecideFunc adapts a plain function to a Decider.
type DecideFunc func(Visit) Decision

// Decide calls f.
func (f DecideFunc) Decide(v Visit) Decision { return f(v) }

// Decorator wraps a Decider to produce another one, e.g. adding a fraud
// guard, metrics, or an A/B override around Decide.
type Decorator func(Decider) Decider

// Chain composes decorators so the first argument runs outermost:
// Chain(a, b, c)(d) == a(b(c(d))). Chain() returns its Decider unchanged.
func Chain(ds ...Decorator) Decorator {
	return func(d Decider) Decider {
		for i := range slices.Backward(ds) {
			d = ds[i](d)
		}
		return d
	}
}
