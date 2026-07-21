package acl_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/acl"
)

// benchStore holds a subject with n specific grants plus one type-wide deny —
// the "manager sees exactly these assigned agents" shape.
func benchStore(b *testing.B, n int) acl.Store {
	b.Helper()
	store := acl.NewMemoryStore()
	m := acl.NewManager(store)
	ctx := context.Background()
	for i := range n {
		if err := m.Grant(ctx, "mgr", "agent", strconv.Itoa(i), "agents:read"); err != nil {
			b.Fatal(err)
		}
	}
	if err := m.Deny(ctx, "mgr", "report", "", "reports:export"); err != nil {
		b.Fatal(err)
	}
	return store
}

func BenchmarkDeciderGrant(b *testing.B) {
	d := acl.Decider(benchStore(b, 100))
	ctx := context.Background()
	sub := access.Subject{ID: "mgr"}
	res := access.Resource{Type: "agent", ID: "42"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = d.Decide(ctx, sub, "agents:read", res)
	}
}

func BenchmarkDeciderDeny(b *testing.B) {
	d := acl.Decider(benchStore(b, 100))
	ctx := context.Background()
	sub := access.Subject{ID: "mgr"}
	res := access.Resource{Type: "report", ID: "7"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = d.Decide(ctx, sub, "reports:export", res)
	}
}

func BenchmarkDeciderAbstain(b *testing.B) {
	d := acl.Decider(benchStore(b, 100))
	ctx := context.Background()
	sub := access.Subject{ID: "nobody"}
	res := access.Resource{Type: "agent", ID: "42"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = d.Decide(ctx, sub, "agents:read", res)
	}
}

func BenchmarkMemoryEntriesFor(b *testing.B) {
	store := benchStore(b, 100)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = store.EntriesFor(ctx, "", "mgr", "agent", "42")
	}
}
