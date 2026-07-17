package smartlink_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/smartlink"
)

var tenantKey = ctxkey.New[string]("tenant")

// scopeFromCtx reads the tenant from ctx, if any; absent context yields an
// empty scope (fails closed downstream, per Manager's WithScope contract).
func scopeFromCtx(ctx context.Context) (string, error) {
	t, _ := tenantKey.From(ctx)
	return t, nil
}

func newScopedManager(t *testing.T, opts ...smartlink.ManagerOption) (*smartlink.Manager, smartlink.Store) {
	t.Helper()
	store := smartlink.NewMemoryStore()
	all := append([]smartlink.ManagerOption{smartlink.WithScope(scopeFromCtx)}, opts...)
	m, err := smartlink.NewManager(store, all...)
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	return m, store
}

// TestScopeFailClosed asserts every management op fails with ErrScope when
// the scope hook returns an empty tenant or an error.
func TestScopeFailClosed(t *testing.T) {
	t.Parallel()

	t.Run("empty scope", func(t *testing.T) {
		t.Parallel()
		m, _ := newScopedManager(t)
		ctx := context.Background() // no tenant in context -> empty scope

		if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"}); !errors.Is(err, smartlink.ErrScope) {
			t.Fatalf("Create() = %v, want ErrScope", err)
		}
		if _, err := m.Get(ctx, "any"); !errors.Is(err, smartlink.ErrScope) {
			t.Fatalf("Get() = %v, want ErrScope", err)
		}
		if _, err := m.List(ctx, smartlink.Filter{}); !errors.Is(err, smartlink.ErrScope) {
			t.Fatalf("List() = %v, want ErrScope", err)
		}
		if err := m.Deactivate(ctx, "any"); !errors.Is(err, smartlink.ErrScope) {
			t.Fatalf("Deactivate() = %v, want ErrScope", err)
		}
		if err := m.Activate(ctx, "any"); !errors.Is(err, smartlink.ErrScope) {
			t.Fatalf("Activate() = %v, want ErrScope", err)
		}
		if err := m.Delete(ctx, "any"); !errors.Is(err, smartlink.ErrScope) {
			t.Fatalf("Delete() = %v, want ErrScope", err)
		}
	})

	t.Run("hook error", func(t *testing.T) {
		t.Parallel()
		hookErr := errors.New("no tenant resolvable")
		m, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithScope(func(context.Context) (string, error) {
			return "", hookErr
		}))
		if err != nil {
			t.Fatalf("NewManager() error = %v, want nil", err)
		}
		ctx := context.Background()
		_, cerr := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"})
		if !errors.Is(cerr, smartlink.ErrScope) {
			t.Fatalf("Create() = %v, want wrapped ErrScope", cerr)
		}
		if !errors.Is(cerr, hookErr) {
			t.Fatalf("Create() = %v, want wrapped hook error", cerr)
		}
	})
}

// TestScopeCreateTenantMismatch asserts Create defaults an empty
// CreateParams.Tenant to the scope tenant, accepts a matching one, and
// rejects a mismatched one with ErrScope.
func TestScopeCreateTenantMismatch(t *testing.T) {
	t.Parallel()
	m, _ := newScopedManager(t)
	ctx := tenantKey.With(context.Background(), "tenant-a")

	l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"})
	if err != nil {
		t.Fatalf("Create(empty tenant) error = %v, want nil", err)
	}
	if l.Tenant != "tenant-a" {
		t.Fatalf("Tenant = %q, want %q", l.Tenant, "tenant-a")
	}

	if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Tenant: "tenant-a"}); err != nil {
		t.Fatalf("Create(matching tenant) error = %v, want nil", err)
	}

	if _, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Tenant: "tenant-b"}); !errors.Is(err, smartlink.ErrScope) {
		t.Fatalf("Create(mismatched tenant) = %v, want ErrScope", err)
	}
}

// TestScopeGetForeignTenant asserts Get on a record owned by a different
// tenant reads as ErrNotFound.
func TestScopeGetForeignTenant(t *testing.T) {
	t.Parallel()
	m, _ := newScopedManager(t)
	ctxA := tenantKey.With(context.Background(), "tenant-a")
	ctxB := tenantKey.With(context.Background(), "tenant-b")

	l, err := m.Create(ctxA, smartlink.CreateParams{Target: "https://example.com/", Code: "foreign1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if _, err := m.Get(ctxB, l.Code); !errors.Is(err, smartlink.ErrNotFound) {
		t.Fatalf("Get(foreign tenant) = %v, want ErrNotFound", err)
	}
	if _, err := m.Get(ctxA, l.Code); err != nil {
		t.Fatalf("Get(owning tenant) error = %v, want nil", err)
	}
}

// TestScopeListForced asserts List overrides Filter.Tenant with the scope
// tenant, ignoring whatever the caller supplied.
func TestScopeListForced(t *testing.T) {
	t.Parallel()
	m, _ := newScopedManager(t)
	ctxA := tenantKey.With(context.Background(), "tenant-a")
	ctxB := tenantKey.With(context.Background(), "tenant-b")

	if _, err := m.Create(ctxA, smartlink.CreateParams{Target: "https://example.com/", Code: "la"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if _, err := m.Create(ctxB, smartlink.CreateParams{Target: "https://example.com/", Code: "lb"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	links, err := m.List(ctxA, smartlink.Filter{Tenant: "tenant-b"})
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(links) != 1 || links[0].Code != "la" {
		t.Fatalf("List() = %+v, want only tenant-a's link", links)
	}
}

// TestScopeMutatorsUsePredicate asserts Deactivate/Delete pass the scope
// tenant as the Store predicate, so a foreign-tenant code is untouched.
func TestScopeMutatorsUsePredicate(t *testing.T) {
	t.Parallel()
	m, store := newScopedManager(t)
	ctxA := tenantKey.With(context.Background(), "tenant-a")
	ctxB := tenantKey.With(context.Background(), "tenant-b")

	if _, err := m.Create(ctxA, smartlink.CreateParams{Target: "https://example.com/", Code: "predicate1"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if err := m.Deactivate(ctxB, "predicate1"); !errors.Is(err, smartlink.ErrNotFound) {
		t.Fatalf("Deactivate(foreign tenant) = %v, want ErrNotFound", err)
	}
	got, err := store.Get(context.Background(), "predicate1")
	if err != nil {
		t.Fatalf("store.Get() error = %v, want nil", err)
	}
	if !got.DeactivatedAt.IsZero() {
		t.Fatal("record was deactivated by foreign tenant")
	}

	if err := m.Delete(ctxB, "predicate1"); !errors.Is(err, smartlink.ErrNotFound) {
		t.Fatalf("Delete(foreign tenant) = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(context.Background(), "predicate1"); err != nil {
		t.Fatalf("record was deleted by foreign tenant: store.Get() = %v", err)
	}

	if err := m.Deactivate(ctxA, "predicate1"); err != nil {
		t.Fatalf("Deactivate(owning tenant) error = %v, want nil", err)
	}
	got, err = store.Get(context.Background(), "predicate1")
	if err != nil {
		t.Fatalf("store.Get() error = %v, want nil", err)
	}
	if got.DeactivatedAt.IsZero() {
		t.Fatal("record was not deactivated by owning tenant")
	}
}

// TestUnscopedPassthrough asserts that without WithScope, tenant strings
// pass through verbatim and management ops work with a plain context.
func TestUnscopedPassthrough(t *testing.T) {
	t.Parallel()
	m := newTestManager(t) // no WithScope
	ctx := context.Background()

	l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Tenant: "any-tenant", Code: "unscoped1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if l.Tenant != "any-tenant" {
		t.Fatalf("Tenant = %q, want passthrough %q", l.Tenant, "any-tenant")
	}

	if _, err := m.Get(ctx, "unscoped1"); err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if err := m.Deactivate(ctx, "unscoped1"); err != nil {
		t.Fatalf("Deactivate() error = %v, want nil", err)
	}
}
