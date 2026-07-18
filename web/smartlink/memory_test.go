package smartlink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/smartlink"
)

// TestMemoryStoreCreateGet asserts a created Link round-trips through Get
// unchanged, and that creating a second Link with the same code fails with
// ErrDuplicate.
func TestMemoryStoreCreateGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := smartlink.NewMemoryStore()
	created := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	l := smartlink.Link{Code: "abc123", Target: "https://example.com/", CreatedAt: created}

	if err := s.Create(ctx, l); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	got, err := s.Get(ctx, "abc123")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if got.Code != l.Code || got.Target != l.Target || !got.CreatedAt.Equal(l.CreatedAt) {
		t.Fatalf("Get() = %+v, want %+v", got, l)
	}

	if err := s.Create(ctx, l); !errors.Is(err, smartlink.ErrDuplicate) {
		t.Fatalf("Create() duplicate = %v, want ErrDuplicate", err)
	}
}

// TestMemoryStoreGetUnknown asserts Get on an absent code returns ErrNotFound.
func TestMemoryStoreGetUnknown(t *testing.T) {
	t.Parallel()
	s := smartlink.NewMemoryStore()
	if _, err := s.Get(context.Background(), "missing"); !errors.Is(err, smartlink.ErrNotFound) {
		t.Fatalf("Get() = %v, want ErrNotFound", err)
	}
}

// TestMemoryStoreListOrder asserts List returns Links ordered by CreatedAt
// descending, code ascending on ties, honors the tenant filter, and caps
// results at Limit (0 meaning no cap).
func TestMemoryStoreListOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := smartlink.NewMemoryStore()
	base := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	links := []smartlink.Link{
		{Code: "b", Tenant: "t1", CreatedAt: base},
		{Code: "a", Tenant: "t1", CreatedAt: base}, // same CreatedAt as "b", tie broken by code
		{Code: "c", Tenant: "t2", CreatedAt: base.Add(time.Hour)},
		{Code: "d", Tenant: "t1", CreatedAt: base.Add(2 * time.Hour)},
	}
	for _, l := range links {
		if err := s.Create(ctx, l); err != nil {
			t.Fatalf("Create(%q) = %v, want nil", l.Code, err)
		}
	}

	all, err := s.List(ctx, smartlink.Filter{})
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	wantOrder := []string{"d", "c", "a", "b"}
	if got := codesOf(all); !equalStrings(got, wantOrder) {
		t.Fatalf("List() codes = %v, want %v", got, wantOrder)
	}

	t1, err := s.List(ctx, smartlink.Filter{Tenant: "t1"})
	if err != nil {
		t.Fatalf("List(tenant) error = %v, want nil", err)
	}
	wantT1 := []string{"d", "a", "b"}
	if got := codesOf(t1); !equalStrings(got, wantT1) {
		t.Fatalf("List(tenant=t1) codes = %v, want %v", got, wantT1)
	}

	limited, err := s.List(ctx, smartlink.Filter{Limit: 2})
	if err != nil {
		t.Fatalf("List(limit) error = %v, want nil", err)
	}
	wantLimited := []string{"d", "c"}
	if got := codesOf(limited); !equalStrings(got, wantLimited) {
		t.Fatalf("List(limit=2) codes = %v, want %v", got, wantLimited)
	}
}

// TestMemoryStoreTenantPredicate asserts Deactivate/Activate/Delete with a
// non-empty tenant apply only when the record belongs to that tenant (else
// ErrNotFound, record untouched), and that an empty tenant is unconstrained.
func TestMemoryStoreTenantPredicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	newStore := func(t *testing.T) smartlink.Store {
		t.Helper()
		s := smartlink.NewMemoryStore()
		if err := s.Create(ctx, smartlink.Link{Code: "code1", Tenant: "owner"}); err != nil {
			t.Fatalf("Create() = %v, want nil", err)
		}
		return s
	}

	t.Run("deactivate wrong tenant", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Deactivate(ctx, "code1", "intruder", at); !errors.Is(err, smartlink.ErrNotFound) {
			t.Fatalf("Deactivate(wrong tenant) = %v, want ErrNotFound", err)
		}
		got, _ := s.Get(ctx, "code1")
		if !got.DeactivatedAt.IsZero() {
			t.Fatalf("record was deactivated despite wrong tenant: %+v", got)
		}
	})

	t.Run("deactivate correct tenant", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Deactivate(ctx, "code1", "owner", at); err != nil {
			t.Fatalf("Deactivate(correct tenant) = %v, want nil", err)
		}
		got, _ := s.Get(ctx, "code1")
		if !got.DeactivatedAt.Equal(at) {
			t.Fatalf("DeactivatedAt = %v, want %v", got.DeactivatedAt, at)
		}
	})

	t.Run("deactivate zero at rejected", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Deactivate(ctx, "code1", "owner", at); err != nil {
			t.Fatalf("Deactivate() = %v, want nil", err)
		}
		// A zero at would store "active"; the contract rejects it before the
		// record is touched, so it can never silently reactivate.
		if err := s.Deactivate(ctx, "code1", "owner", time.Time{}); !errors.Is(err, smartlink.ErrInvalidLink) {
			t.Fatalf("Deactivate(zero at) = %v, want ErrInvalidLink", err)
		}
		got, _ := s.Get(ctx, "code1")
		if !got.DeactivatedAt.Equal(at) {
			t.Fatalf("DeactivatedAt = %v, want unchanged %v", got.DeactivatedAt, at)
		}
	})

	t.Run("activate wrong tenant", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Deactivate(ctx, "code1", "owner", at); err != nil {
			t.Fatalf("Deactivate() = %v, want nil", err)
		}
		if err := s.Activate(ctx, "code1", "intruder"); !errors.Is(err, smartlink.ErrNotFound) {
			t.Fatalf("Activate(wrong tenant) = %v, want ErrNotFound", err)
		}
		got, _ := s.Get(ctx, "code1")
		if got.DeactivatedAt.IsZero() {
			t.Fatalf("record was activated despite wrong tenant: %+v", got)
		}
	})

	t.Run("activate empty tenant unconstrained", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Deactivate(ctx, "code1", "owner", at); err != nil {
			t.Fatalf("Deactivate() = %v, want nil", err)
		}
		if err := s.Activate(ctx, "code1", ""); err != nil {
			t.Fatalf("Activate(empty tenant) = %v, want nil", err)
		}
		got, _ := s.Get(ctx, "code1")
		if !got.DeactivatedAt.IsZero() {
			t.Fatalf("DeactivatedAt = %v, want zero after Activate", got.DeactivatedAt)
		}
	})

	t.Run("delete wrong tenant", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Delete(ctx, "code1", "intruder"); !errors.Is(err, smartlink.ErrNotFound) {
			t.Fatalf("Delete(wrong tenant) = %v, want ErrNotFound", err)
		}
		if _, err := s.Get(ctx, "code1"); err != nil {
			t.Fatalf("record was deleted despite wrong tenant: Get() = %v", err)
		}
	})

	t.Run("delete correct tenant", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Delete(ctx, "code1", "owner"); err != nil {
			t.Fatalf("Delete(correct tenant) = %v, want nil", err)
		}
		if _, err := s.Get(ctx, "code1"); !errors.Is(err, smartlink.ErrNotFound) {
			t.Fatalf("Get() after delete = %v, want ErrNotFound", err)
		}
	})
}

// TestMemoryStoreMetadataAliasing asserts Metadata maps are cloned on both
// write and read: mutating the caller's map after Create, and mutating a map
// returned by Get, must not affect stored state.
func TestMemoryStoreMetadataAliasing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := smartlink.NewMemoryStore()
	meta := map[string]string{"k": "v1"}
	if err := s.Create(ctx, smartlink.Link{Code: "code1", Metadata: meta}); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	meta["k"] = "mutated-by-caller"
	got, err := s.Get(ctx, "code1")
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got.Metadata["k"] != "v1" {
		t.Fatalf("Metadata[k] = %q, want %q (caller mutation leaked into store)", got.Metadata["k"], "v1")
	}

	got.Metadata["k"] = "mutated-by-reader"
	got2, err := s.Get(ctx, "code1")
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got2.Metadata["k"] != "v1" {
		t.Fatalf("Metadata[k] = %q, want %q (reader mutation leaked into store)", got2.Metadata["k"], "v1")
	}
}

// TestMemoryStoreDeleteRecreate asserts a code freed by Delete can be
// immediately reused by Create.
func TestMemoryStoreDeleteRecreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := smartlink.NewMemoryStore()
	if err := s.Create(ctx, smartlink.Link{Code: "code1", Target: "https://a.example.com/"}); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if err := s.Delete(ctx, "code1", ""); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}
	if err := s.Create(ctx, smartlink.Link{Code: "code1", Target: "https://b.example.com/"}); err != nil {
		t.Fatalf("Create() after delete = %v, want nil", err)
	}
	got, err := s.Get(ctx, "code1")
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got.Target != "https://b.example.com/" {
		t.Fatalf("Target = %q, want new target after recreate", got.Target)
	}
}

func codesOf(links []smartlink.Link) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l.Code
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
