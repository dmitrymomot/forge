package session_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
)

// tenantCtxKey is a private context key so tests never collide with a real
// application's own context values.
type tenantCtxKey struct{}

// withTenant returns a context carrying tenant, read by scopeFromCtx.
func withTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, tenant)
}

// scopeFromCtx is a WithScope hook that reads the tenant a test context
// carries. A context with no tenant set yields "", which — for a configured
// hook — is the fail-closed empty-scope case, not "unscoped".
func scopeFromCtx(ctx context.Context) (string, error) {
	t, _ := ctx.Value(tenantCtxKey{}).(string)
	return t, nil
}

// erroringScope always fails, for the hook-error fail-closed proofs.
func erroringScope(context.Context) (string, error) {
	return "", errors.New("scope: boom")
}

func TestScopeStampsTenantOnSave(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store), session.WithScope(scopeFromCtx))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if err := mgr.Save(withTenant(t.Context(), "t1"), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec, err := store.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Tenant != "t1" {
		t.Fatalf("Record.Tenant = %q, want %q", rec.Tenant, "t1")
	}
}

// TestScopeCrossTenantLoadIsNotFound is the core isolation proof: a token
// saved under one tenant must never load under another, and the failure must
// be indistinguishable from a token that never existed.
func TestScopeCrossTenantLoadIsNotFound(t *testing.T) {
	mgr := newTestManager(t, session.WithScope(scopeFromCtx))
	ctxT1 := withTenant(t.Context(), "t1")
	ctxT2 := withTenant(t.Context(), "t2")

	sess := mgr.Start()
	if err := mgr.Save(ctxT1, sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := mgr.Load(ctxT2, sess.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load under t2 of a t1 token = %v, want ErrNotFound", err)
	}

	if _, err := mgr.Load(ctxT1, sess.Token()); err != nil {
		t.Fatalf("Load under the owning tenant t1: %v", err)
	}
}

func TestScopeFailClosedOnHookError(t *testing.T) {
	store := session.NewMemoryStore()
	seeder, err := session.New(session.DefaultConfig(), session.WithStore(store), session.WithScope(scopeFromCtx))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	existing := seeder.Start()
	if err := seeder.Save(withTenant(t.Context(), "t1"), existing); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	broken, err := session.New(session.DefaultConfig(), session.WithStore(store), session.WithScope(erroringScope))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := broken.Load(t.Context(), existing.Token()); !errors.Is(err, session.ErrScope) {
		t.Fatalf("Load with an erroring scope hook = %v, want ErrScope", err)
	}

	fresh := broken.Start()
	if err := broken.Save(t.Context(), fresh); !errors.Is(err, session.ErrScope) {
		t.Fatalf("Save with an erroring scope hook = %v, want ErrScope", err)
	}
	if _, err := store.Load(t.Context(), fresh.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("a failed scope resolution must write no row")
	}
}

func TestScopeFailClosedOnEmptyScope(t *testing.T) {
	mgr := newTestManager(t, session.WithScope(scopeFromCtx))
	sess := mgr.Start()

	// t.Context() carries no tenant value, so scopeFromCtx resolves "" — an
	// empty scope from a configured hook, which must fail closed, not be
	// treated as "unscoped".
	if err := mgr.Save(t.Context(), sess); !errors.Is(err, session.ErrScope) {
		t.Fatalf("Save with an empty-resolving scope hook = %v, want ErrScope", err)
	}
}

// TestFailedScopeSaveKeepsPendingPayloadForRetry mirrors
// TestFailedAuthenticateKeepsPendingPayloadForRetry: it pins that a Save which
// fails during scope resolution — before encode() or the store write ever run —
// leaves a dirty namespace write pending rather than silently dropping it, so a
// retry under a working scope still flushes it.
func TestFailedScopeSaveKeepsPendingPayloadForRetry(t *testing.T) {
	store := session.NewMemoryStore()
	var failing atomic.Bool
	failing.Store(true)
	hook := func(context.Context) (string, error) {
		if failing.Load() {
			return "", errors.New("scope: boom")
		}
		return "t1", nil
	}
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store), session.WithScope(hook))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	nsCart.Set(sess, cartData{Items: []string{"guest-item"}})

	if err := mgr.Save(t.Context(), sess); !errors.Is(err, session.ErrScope) {
		t.Fatalf("Save with a failing scope hook = %v, want ErrScope", err)
	}
	if _, err := store.Load(t.Context(), sess.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("a failed scope resolution must write no row")
	}

	failing.Store(false)
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("retry Save: %v", err)
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cart, err := nsCart.Get(reloaded)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(cart.Items) != 1 || cart.Items[0] != "guest-item" {
		t.Fatalf("pending namespace write lost after a scope-failed Save: %+v", cart)
	}
}

func TestScopeConfinesListByUser(t *testing.T) {
	mgr := newTestManager(t, session.WithScope(scopeFromCtx))
	ctxT1 := withTenant(t.Context(), "t1")
	ctxT2 := withTenant(t.Context(), "t2")

	for range 2 {
		s := mgr.Start()
		if err := mgr.Authenticate(ctxT1, s, "u1"); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}
	other := mgr.Start()
	if err := mgr.Authenticate(ctxT2, other, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	list, err := mgr.ListByUser(ctxT1, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListByUser under t1 returned %d records, want 2", len(list))
	}
	for _, r := range list {
		if r.Tenant != "t1" {
			t.Fatalf("ListByUser under t1 leaked a %q record", r.Tenant)
		}
	}
}

// TestScopeConfinesRevoke constructs a genuine cross-tenant collision — same
// user id AND same session id, owned by different tenants — and proves
// Revoke under one tenant cannot touch the other's record.
func TestScopeConfinesRevoke(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store), session.WithScope(scopeFromCtx))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sameID := id.NewUUID()
	now := time.Now().UTC()
	if _, err := store.Create(t.Context(), "tok-t1", session.Record{
		ID: sameID, UserID: "u1", Tenant: "t1",
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed t1: %v", err)
	}
	if _, err := store.Create(t.Context(), "tok-t2", session.Record{
		ID: sameID, UserID: "u1", Tenant: "t2",
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed t2: %v", err)
	}

	if err := mgr.Revoke(withTenant(t.Context(), "t1"), "u1", sameID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := store.Load(t.Context(), "tok-t1"); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("Revoke under t1 must remove the t1 session")
	}
	if _, err := store.Load(t.Context(), "tok-t2"); err != nil {
		t.Fatalf("Revoke under t1 must not touch t2's session, even with the same user+session id: %v", err)
	}
}

func TestScopeConfinesLogoutOthers(t *testing.T) {
	mgr := newTestManager(t, session.WithScope(scopeFromCtx))
	ctxT1 := withTenant(t.Context(), "t1")
	ctxT2 := withTenant(t.Context(), "t2")

	current := mgr.Start()
	if err := mgr.Authenticate(ctxT1, current, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	other := mgr.Start()
	if err := mgr.Authenticate(ctxT1, other, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	t2Sess := mgr.Start()
	if err := mgr.Authenticate(ctxT2, t2Sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if err := mgr.LogoutOthers(ctxT1, current); err != nil {
		t.Fatalf("LogoutOthers: %v", err)
	}

	if _, err := mgr.Load(ctxT1, other.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("LogoutOthers under t1 must remove the other t1 session")
	}
	if _, err := mgr.Load(ctxT2, t2Sess.Token()); err != nil {
		t.Fatalf("LogoutOthers under t1 must not touch t2's session: %v", err)
	}
}

func TestNoScopeIsSingleTenantZeroCeremony(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec, err := store.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Tenant != "" {
		t.Fatalf("Record.Tenant = %q, want empty with no WithScope configured", rec.Tenant)
	}

	list, err := mgr.ListByUser(t.Context(), "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListByUser = %d records, want 0", len(list))
	}
}
