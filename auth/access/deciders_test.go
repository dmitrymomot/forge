package access_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
)

func run(t *testing.T, d access.Decider, s access.Subject, a access.Action, r access.Resource) access.Decision {
	t.Helper()
	got, err := d.Decide(context.Background(), s, a, r)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	return got
}

func TestScopeDeciderAllowsWhenScopePresent(t *testing.T) {
	s := access.Subject{Scopes: []string{"documents:read", "documents:write"}}
	if got := run(t, access.ScopeDecider(), s, "documents:read", access.Resource{}); got.Effect != access.Allow {
		t.Fatalf("got %+v", got)
	}
}

func TestScopeDeciderAbstainsWhenScopeAbsent(t *testing.T) {
	s := access.Subject{Scopes: []string{"documents:read"}}
	if got := run(t, access.ScopeDecider(), s, "documents:write", access.Resource{}); got.Effect != access.Abstain {
		t.Fatalf("got %+v", got)
	}
}

func TestTenantMatchDeniesCrossTenant(t *testing.T) {
	s := access.Subject{Tenant: "t1"}
	r := access.Resource{Tenant: "t2"}
	if got := run(t, access.TenantMatch(), s, "a", r); got.Effect != access.Deny || got.Decider != "tenant" {
		t.Fatalf("got %+v", got)
	}
}

func TestTenantMatchAbstainsSameTenant(t *testing.T) {
	s := access.Subject{Tenant: "t1"}
	r := access.Resource{Tenant: "t1"}
	if got := run(t, access.TenantMatch(), s, "a", r); got.Effect != access.Abstain {
		t.Fatalf("got %+v", got)
	}
}

func TestTenantMatchAbstainsWhenEitherEmpty(t *testing.T) {
	// single-tenant app: both empty -> abstain (zero ceremony)
	if got := run(t, access.TenantMatch(), access.Subject{}, "a", access.Resource{}); got.Effect != access.Abstain {
		t.Fatalf("both empty: got %+v", got)
	}
	// resource not tenant-scoped -> abstain
	if got := run(t, access.TenantMatch(), access.Subject{Tenant: "t1"}, "a", access.Resource{}); got.Effect != access.Abstain {
		t.Fatalf("resource empty: got %+v", got)
	}
	// subject not tenant-scoped -> abstain
	if got := run(t, access.TenantMatch(), access.Subject{}, "a", access.Resource{Tenant: "t1"}); got.Effect != access.Abstain {
		t.Fatalf("subject empty: got %+v", got)
	}
}

func TestTerminals(t *testing.T) {
	if got := run(t, access.AllowAll(), access.Subject{}, "a", access.Resource{}); got.Effect != access.Allow {
		t.Fatalf("AllowAll got %+v", got)
	}
	got := run(t, access.DenyAll("nope"), access.Subject{}, "a", access.Resource{})
	if got.Effect != access.Deny || got.Reason != "nope" {
		t.Fatalf("DenyAll got %+v", got)
	}
}
