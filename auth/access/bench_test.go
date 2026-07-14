package access_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
)

func benchInputs() (access.Decider, access.Subject, access.Resource) {
	d := access.FirstDecisive(access.TenantMatch(), access.ScopeDecider())
	s := access.Subject{ID: "u1", Tenant: "t1", Scopes: []string{"documents:read"}}
	r := access.Resource{Type: "document", ID: "42", Tenant: "t1"}
	return d, s, r
}

func BenchmarkFirstDecisiveScope(b *testing.B) {
	d, s, r := benchInputs()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.Decide(ctx, s, "documents:read", r)
	}
}

func TestDecidePathIsZeroAlloc(t *testing.T) {
	d, s, r := benchInputs()
	ctx := context.Background()
	allocs := testing.AllocsPerRun(200, func() {
		_, _ = d.Decide(ctx, s, "documents:read", r)
	})
	if allocs != 0 {
		t.Fatalf("Decide path must be zero-alloc, got %v allocs/op", allocs)
	}
}
