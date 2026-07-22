package smartlink

import "slices"

// Decider makes the per-click decision. *Compiled implements it; decorators
// (fraud guards, metrics, A/B overrides) wrap a Decider to build another one.
// An error refuses the click fail-closed — [Manager.Handler] answers 403 for
// [ErrMissingFact] and 500 for anything else, never a redirect — so a
// decorator may also reject a visit outright instead of inventing a decision.
type Decider interface {
	Decide(Visit) (Decision, error)
}

// DecideFunc adapts a plain function to a Decider.
type DecideFunc func(Visit) (Decision, error)

// Decide calls f.
func (f DecideFunc) Decide(v Visit) (Decision, error) { return f(v) }

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
