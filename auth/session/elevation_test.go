package session_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

func TestRequireElevationAbstainsOnUnlistedActions(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	ctx := session.TestWithSession(t.Context(), sess)

	d := session.RequireElevation(10*time.Minute, "tenant:delete")

	dec, err := d.Decide(ctx, access.Subject{ID: "u1"}, "project:read", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Abstain {
		t.Fatalf("effect = %v for an unlisted action, want Abstain — an unscoped decider would deny every action in the app", dec.Effect)
	}
}

func TestRequireElevationDeniesWhenStale(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk))

	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	ctx := session.TestWithSession(t.Context(), sess)

	d := session.RequireElevation(10*time.Minute, "tenant:delete")

	dec, err := d.Decide(ctx, access.Subject{ID: "u1"}, "tenant:delete", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Abstain {
		t.Fatalf("effect = %v for a freshly elevated session, want Abstain so rbac can allow", dec.Effect)
	}

	clk.Advance(20 * time.Minute)
	dec, err = d.Decide(ctx, access.Subject{ID: "u1"}, "tenant:delete", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Deny {
		t.Fatalf("effect = %v for a stale elevation, want Deny", dec.Effect)
	}
	if dec.Reason == "" {
		t.Fatal("a denial must carry a reason for logs and the forbidden handler")
	}
}

// TestRequireElevationDeniesAForeignSubject pins the principal binding: in a
// mixed-auth chain the subject under decision can come from another mechanism
// (an API key, say), and a bystander session's elevation must not vouch for it.
func TestRequireElevationDeniesAForeignSubject(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if err := mgr.Elevate(t.Context(), sess); err != nil {
		t.Fatalf("Elevate: %v", err)
	}
	ctx := session.TestWithSession(t.Context(), sess)

	d := session.RequireElevation(10*time.Minute, "tenant:delete")
	dec, err := d.Decide(ctx, access.Subject{ID: "api-key-service-account"}, "tenant:delete", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Deny {
		t.Fatalf("effect = %v for a subject that is not the session's user, want Deny", dec.Effect)
	}
}

func TestRequireElevationDeniesWithoutASession(t *testing.T) {
	d := session.RequireElevation(10*time.Minute, "tenant:delete")

	dec, err := d.Decide(t.Context(), access.Subject{ID: "u1"}, "tenant:delete", access.Resource{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Effect != access.Deny {
		t.Fatalf("effect = %v with no session in context, want Deny — fail closed", dec.Effect)
	}
}
