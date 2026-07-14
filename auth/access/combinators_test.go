package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
)

func constDecider(name string, e access.Effect) access.Decider {
	return access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{Effect: e, Decider: name}, nil
	})
}

func errDecider(name string, err error) access.Decider {
	return access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{Decider: name}, err
	})
}

func decide(t *testing.T, d access.Decider) access.Decision {
	t.Helper()
	got, err := d.Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	return got
}

func TestFirstDecisiveFirstWins(t *testing.T) {
	d := access.FirstDecisive(
		constDecider("acl", access.Abstain),
		constDecider("rbac", access.Allow),
		constDecider("late", access.Deny),
	)
	got := decide(t, d)
	if got.Effect != access.Allow || got.Decider != "rbac" {
		t.Fatalf("got %+v", got)
	}
}

func TestFirstDecisiveDenyBeatsLaterAllow(t *testing.T) {
	d := access.FirstDecisive(
		constDecider("acl", access.Deny),
		constDecider("rbac", access.Allow),
	)
	if got := decide(t, d); got.Effect != access.Deny || got.Decider != "acl" {
		t.Fatalf("got %+v", got)
	}
}

func TestFirstDecisiveAllAbstain(t *testing.T) {
	d := access.FirstDecisive(constDecider("a", access.Abstain), constDecider("b", access.Abstain))
	if got := decide(t, d); got.Effect != access.Abstain {
		t.Fatalf("got %+v", got)
	}
}

func TestFirstDecisiveErrorFailsClosed(t *testing.T) {
	sentinel := errors.New("boom")
	d := access.FirstDecisive(
		constDecider("acl", access.Abstain),
		errDecider("rbac", sentinel),
		constDecider("late", access.Allow),
	)
	got, err := d.Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if !errors.Is(err, sentinel) || got.Effect != access.Deny {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestDenyOverridesVetoRegardlessOfOrder(t *testing.T) {
	d := access.DenyOverrides(
		constDecider("rbac", access.Allow),
		constDecider("acl", access.Deny), // later, still vetoes
	)
	if got := decide(t, d); got.Effect != access.Deny || got.Decider != "acl" {
		t.Fatalf("got %+v", got)
	}
}

func TestDenyOverridesFirstAllowWhenNoDeny(t *testing.T) {
	d := access.DenyOverrides(
		constDecider("a", access.Abstain),
		constDecider("b", access.Allow),
		constDecider("c", access.Allow),
	)
	if got := decide(t, d); got.Effect != access.Allow || got.Decider != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestWithExplainPopulatesTrace(t *testing.T) {
	d := access.FirstDecisive(
		constDecider("acl", access.Abstain),
		constDecider("rbac", access.Allow),
	)
	ctx := access.WithExplain(context.Background())
	got, _ := d.Decide(ctx, access.Subject{}, "a", access.Resource{})
	if len(got.Trace) != 2 {
		t.Fatalf("want trace of 2, got %d: %+v", len(got.Trace), got.Trace)
	}
	if got.Trace[0].Decider != "acl" || got.Trace[1].Decider != "rbac" {
		t.Fatalf("trace order wrong: %+v", got.Trace)
	}
}

func TestNoTraceWithoutExplain(t *testing.T) {
	d := access.FirstDecisive(constDecider("acl", access.Allow))
	if got := decide(t, d); got.Trace != nil {
		t.Fatalf("trace must be nil without WithExplain, got %+v", got)
	}
}

func TestDenyOverridesTraceEndsAtVeto(t *testing.T) {
	d := access.DenyOverrides(
		constDecider("a", access.Allow),
		constDecider("b", access.Deny),
		constDecider("c", access.Allow),
	)
	ctx := access.WithExplain(context.Background())
	got, err := d.Decide(ctx, access.Subject{}, "act", access.Resource{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Effect != access.Deny || got.Decider != "b" {
		t.Fatalf("want deny by b, got %+v", got)
	}
	if len(got.Trace) != 2 {
		t.Fatalf("trace must end at the veto (a,b), got %d: %+v", len(got.Trace), got.Trace)
	}
	if got.Trace[0].Decider != "a" || got.Trace[1].Decider != "b" {
		t.Fatalf("trace order wrong: %+v", got.Trace)
	}
}
