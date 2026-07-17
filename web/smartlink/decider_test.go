package smartlink_test

import (
	"testing"

	"github.com/dmitrymomot/forge/web/smartlink"
)

// TestCompiledIsDecider asserts *Compiled satisfies Decider and that a
// compiled link decides through the interface, not just the concrete type.
func TestCompiledIsDecider(t *testing.T) {
	t.Parallel()
	var _ smartlink.Decider = (*smartlink.Compiled)(nil)

	link := mustCompile(t, smartlink.Spec{Default: defTargets()})
	var d smartlink.Decider = link
	if got := d.Decide(smartlink.Visit{}).URL; got != "https://example.com/" {
		t.Fatalf("Decide via interface = %q, want default target", got)
	}
}

// tagDecorator returns a Decorator that appends tag to Decision.Rule before
// delegating to the wrapped Decider, so composition order is observable.
func tagDecorator(tag string) smartlink.Decorator {
	return func(next smartlink.Decider) smartlink.Decider {
		return smartlink.DecideFunc(func(v smartlink.Visit) smartlink.Decision {
			d := next.Decide(v)
			d.Rule += tag
			return d
		})
	}
}

// TestChainOrder asserts Chain(A, B, C)(d) == A(B(C(d))): C runs closest to
// d, and A's tag is applied last, ending up outermost in the result.
func TestChainOrder(t *testing.T) {
	t.Parallel()
	base := smartlink.DecideFunc(func(smartlink.Visit) smartlink.Decision {
		return smartlink.Decision{Rule: "base"}
	})
	chained := smartlink.Chain(tagDecorator("A"), tagDecorator("B"), tagDecorator("C"))(base)
	got := chained.Decide(smartlink.Visit{}).Rule
	want := "baseCBA"
	if got != want {
		t.Fatalf("Chain(A, B, C)(base).Decide().Rule = %q, want %q", got, want)
	}
}

// TestChainEmpty asserts Chain() with no decorators returns the wrapped
// Decider's decisions unchanged.
func TestChainEmpty(t *testing.T) {
	t.Parallel()
	base := smartlink.DecideFunc(func(smartlink.Visit) smartlink.Decision {
		return smartlink.Decision{Rule: "base", URL: "https://example.com/"}
	})
	got := smartlink.Chain()(base).Decide(smartlink.Visit{})
	want := smartlink.Decision{Rule: "base", URL: "https://example.com/"}
	if got != want {
		t.Fatalf("Chain()(base).Decide() = %+v, want %+v", got, want)
	}
}
