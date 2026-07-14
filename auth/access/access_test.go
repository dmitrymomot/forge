package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/guard"
)

func TestEffectBecause(t *testing.T) {
	d := access.Allow.Because("ok")
	if d.Effect != access.Allow || d.Reason != "ok" {
		t.Fatalf("got %+v", d)
	}
}

func TestDeciderFuncImplementsDecider(t *testing.T) {
	var d access.Decider = access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Allow.Because("yes"), nil
	})
	got, err := d.Decide(context.Background(), access.Subject{}, "act", access.Resource{})
	if err != nil || got.Effect != access.Allow {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestNamedStampsEmptyDeciderOnly(t *testing.T) {
	inner := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Allow.Because("r"), nil // Decider left empty
	})
	got, _ := access.Named("role", inner).Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if got.Decider != "role" {
		t.Fatalf("want stamped role, got %q", got.Decider)
	}

	preset := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{Effect: access.Deny, Decider: "acl", Reason: "r"}, nil
	})
	got, _ = access.Named("role", preset).Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if got.Decider != "acl" {
		t.Fatalf("want preserved acl, got %q", got.Decider)
	}
}

func TestSubjectFromIdentity(t *testing.T) {
	id := guard.Identity{Subject: "u1", Tenant: "t1", Scopes: []string{"a", "b"}, Meta: map[string]string{"email": "x"}}
	s := access.SubjectFromIdentity(id)
	if s.ID != "u1" || s.Tenant != "t1" || len(s.Scopes) != 2 {
		t.Fatalf("got %+v", s)
	}
	if s.Attrs != nil {
		t.Fatalf("Attrs must stay nil (no Meta promotion), got %v", s.Attrs)
	}
}

func TestAuthorizeAllowPassthrough(t *testing.T) {
	d := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Allow.Because("ok"), nil
	})
	got, err := access.Authorize(context.Background(), d, access.Subject{}, "a", access.Resource{})
	if err != nil || got.Effect != access.Allow {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestAuthorizeAbstainBecomesDeny(t *testing.T) {
	d := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, nil // Abstain
	})
	got, err := access.Authorize(context.Background(), d, access.Subject{}, "a", access.Resource{})
	if err != nil || got.Effect != access.Deny || got.Decider != "access" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestAuthorizeErrorFailsClosed(t *testing.T) {
	sentinel := errors.New("store down")
	d := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, sentinel
	})
	got, err := access.Authorize(context.Background(), d, access.Subject{}, "a", access.Resource{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if got.Effect != access.Deny {
		t.Fatalf("want fail-closed Deny, got %+v", got)
	}
}

func TestDecisionFromRoundTrip(t *testing.T) {
	if _, ok := access.DecisionFrom(context.Background()); ok {
		t.Fatal("empty ctx must report ok=false")
	}
}
