package otp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/otp"
)

// The tests below are executable proof that a single WithScope construction
// seam serves the three tenancy shapes a real application needs: a
// single-tenant app, a white-label app where every user is locked to one
// tenant, and a platform where a global user switches between tenants. The
// scope hook is arbitrary app logic mapping the request context to a scope
// string, so "switching tenants" is nothing more than a different context per
// request.

// TestTenancy_SingleTenant: omit WithScope entirely. Scope resolves to "" and
// every identifier shares one flat namespace — zero ceremony.
func TestTenancy_SingleTenant(t *testing.T) {
	t.Parallel()
	o, err := otp.New(testSecret, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	code, err := o.Generate(t.Context(), "user@example.com")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := o.Verify(t.Context(), "user@example.com", code); err != nil {
		t.Fatalf("single-tenant verify: %v", err)
	}
}

// TestTenancy_WhiteLabelTenantLocked: every request is bound to exactly one
// tenant. The same identifier in a sibling tenant is a different code, and a
// request that arrives without a tenant fails closed rather than leaking into a
// shared bucket.
func TestTenancy_WhiteLabelTenantLocked(t *testing.T) {
	t.Parallel()
	scope := func(ctx context.Context) (string, error) {
		tenant, _ := ctx.Value(ctxTenant{}).(string)
		return tenant, nil // "" -> resolveScope fails closed with ErrScope
	}
	o, err := otp.New(testSecret, newStore(t), otp.WithScope(scope))
	if err != nil {
		t.Fatal(err)
	}

	acme := context.WithValue(t.Context(), ctxTenant{}, "acme")
	globex := context.WithValue(t.Context(), ctxTenant{}, "globex")

	code, err := o.Generate(acme, "user@example.com")
	if err != nil {
		t.Fatalf("Generate under acme: %v", err)
	}
	// A sibling white-label tenant with the SAME identifier never sees the code.
	if err := o.Verify(globex, "user@example.com", code); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("cross-tenant verify = %v, want ErrNotFound", err)
	}
	// The owning tenant verifies.
	if err := o.Verify(acme, "user@example.com", code); err != nil {
		t.Fatalf("owning-tenant verify: %v", err)
	}
	// A request that lost its tenant fails closed — no fall-through to a global
	// bucket, on Generate, Verify, and Revoke alike.
	if _, err := o.Generate(t.Context(), "user@example.com"); !errors.Is(err, otp.ErrScope) {
		t.Fatalf("tenantless Generate = %v, want ErrScope", err)
	}
	if err := o.Verify(t.Context(), "user@example.com", code); !errors.Is(err, otp.ErrScope) {
		t.Fatalf("tenantless Verify = %v, want ErrScope", err)
	}
	if err := o.Revoke(t.Context(), "user@example.com"); !errors.Is(err, otp.ErrScope) {
		t.Fatalf("tenantless Revoke = %v, want ErrScope", err)
	}
}

// TestTenancy_GlobalUserSwitchingTenants: one global identity operates at the
// platform level AND inside tenants it switches into. The app's hook returns
// the active tenant when the user is inside one, and a reserved non-empty
// global sentinel at the platform level. Codes issued in one scope are
// isolated from every other scope.
func TestTenancy_GlobalUserSwitchingTenants(t *testing.T) {
	t.Parallel()
	const globalScope = "@global" // reserved; must not equal any real tenant id
	scope := func(ctx context.Context) (string, error) {
		if tenant, ok := ctx.Value(ctxTenant{}).(string); ok && tenant != "" {
			return tenant, nil // acting inside a tenant the user switched into
		}
		return globalScope, nil // platform level — a global, non-empty bucket
	}
	o, err := otp.New(testSecret, newStore(t), otp.WithScope(scope))
	if err != nil {
		t.Fatal(err)
	}

	platform := t.Context()                                     // no tenant selected
	acme := context.WithValue(t.Context(), ctxTenant{}, "acme") // switched in

	// A platform-level code lives in the global bucket, isolated from tenants:
	// switching into acme cannot verify a platform-issued code.
	gcode, err := o.Generate(platform, "admin@platform.com")
	if err != nil {
		t.Fatalf("platform Generate: %v", err)
	}
	if err := o.Verify(acme, "admin@platform.com", gcode); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("global code inside tenant = %v, want ErrNotFound", err)
	}
	if err := o.Verify(platform, "admin@platform.com", gcode); err != nil {
		t.Fatalf("platform verify: %v", err)
	}

	// After switching into acme, the SAME user gets an acme-scoped code that is
	// independent of anything at the platform level.
	tcode, err := o.Generate(acme, "admin@platform.com")
	if err != nil {
		t.Fatalf("acme Generate: %v", err)
	}
	if err := o.Verify(platform, "admin@platform.com", tcode); !errors.Is(err, otp.ErrNotFound) {
		t.Fatalf("tenant code at platform = %v, want ErrNotFound", err)
	}
	if err := o.Verify(acme, "admin@platform.com", tcode); err != nil {
		t.Fatalf("acme verify: %v", err)
	}
}

// TestTenancy_SentinelTenantCollision makes the doc's disjointness caveat
// verifiable: if the reserved global sentinel is NOT kept distinct from real
// tenant IDs, a global user and the colliding tenant share one bucket. This
// test deliberately violates the rule to prove the failure mode exists —
// production apps must keep the sentinel disjoint from tenant IDs.
func TestTenancy_SentinelTenantCollision(t *testing.T) {
	t.Parallel()
	const sentinel = "@global"
	scope := func(ctx context.Context) (string, error) {
		if tenant, ok := ctx.Value(ctxTenant{}).(string); ok && tenant != "" {
			return tenant, nil
		}
		return sentinel, nil
	}
	o, err := otp.New(testSecret, newStore(t), otp.WithScope(scope))
	if err != nil {
		t.Fatal(err)
	}
	platform := t.Context()                                            // resolves to sentinel
	colliding := context.WithValue(t.Context(), ctxTenant{}, sentinel) // tenant id == sentinel

	code, err := o.Generate(platform, "admin@platform.com")
	if err != nil {
		t.Fatalf("platform Generate: %v", err)
	}
	// Same identifier + same resolved scope -> same bucket, so a code issued at
	// the platform level IS visible to the colliding tenant. That shared
	// visibility is exactly why the sentinel must stay disjoint from tenant IDs.
	if err := o.Verify(colliding, "admin@platform.com", code); err != nil {
		t.Fatalf("collision: verify = %v, want success (proves the shared-bucket failure mode)", err)
	}
}
