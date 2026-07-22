package smartlink_test

import (
	"errors"
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
	dec, err := d.Decide(smartlink.Visit{})
	if err != nil {
		t.Fatalf("Decide via interface error = %v", err)
	}
	if dec.URL != "https://example.com/" {
		t.Fatalf("Decide via interface = %q, want default target", dec.URL)
	}
}

// tagDecorator returns a Decorator that appends tag to Decision.Rule before
// delegating to the wrapped Decider, so composition order is observable.
func tagDecorator(tag string) smartlink.Decorator {
	return func(next smartlink.Decider) smartlink.Decider {
		return smartlink.DecideFunc(func(v smartlink.Visit) (smartlink.Decision, error) {
			d, err := next.Decide(v)
			if err != nil {
				return smartlink.Decision{}, err
			}
			d.Rule += tag
			return d, nil
		})
	}
}

// TestChainOrder asserts Chain(A, B, C)(d) == A(B(C(d))): C runs closest to
// d, and A's tag is applied last, ending up outermost in the result.
func TestChainOrder(t *testing.T) {
	t.Parallel()
	base := smartlink.DecideFunc(func(smartlink.Visit) (smartlink.Decision, error) {
		return smartlink.Decision{Rule: "base"}, nil
	})
	chained := smartlink.Chain(tagDecorator("A"), tagDecorator("B"), tagDecorator("C"))(base)
	got, err := chained.Decide(smartlink.Visit{})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if want := "baseCBA"; got.Rule != want {
		t.Fatalf("Chain(A, B, C)(base).Decide().Rule = %q, want %q", got.Rule, want)
	}
}

// TestChainEmpty asserts Chain() with no decorators returns the wrapped
// Decider's decisions unchanged.
func TestChainEmpty(t *testing.T) {
	t.Parallel()
	base := smartlink.DecideFunc(func(smartlink.Visit) (smartlink.Decision, error) {
		return smartlink.Decision{Rule: "base", URL: "https://example.com/"}, nil
	})
	got, err := smartlink.Chain()(base).Decide(smartlink.Visit{})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	want := smartlink.Decision{Rule: "base", URL: "https://example.com/"}
	if got != want {
		t.Fatalf("Chain()(base).Decide() = %+v, want %+v", got, want)
	}
}

// TestChainPropagatesError asserts a wrapped Decider's refusal surfaces
// through the decorator chain instead of being swallowed into a decision.
func TestChainPropagatesError(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules:   []smartlink.Rule{rule("geo", "https://hit.com", smartlink.Geo{Countries: []string{"DE"}})},
		Default: defTargets(),
	})
	chained := smartlink.Chain(tagDecorator("A"))(link)
	if _, err := chained.Decide(smartlink.Visit{}); !errors.Is(err, smartlink.ErrMissingFact) {
		t.Fatalf("Decide() error = %v, want ErrMissingFact through the chain", err)
	}
}
