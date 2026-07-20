package abac_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/dmitrymomot/forge/auth/abac"
	"github.com/dmitrymomot/forge/auth/access"
)

func benchPolicy(b *testing.B) *abac.Policy {
	b.Helper()
	p, err := abac.New(abac.WithRules(
		abac.Deny("archived-write", "documents:write", "document",
			func(_ context.Context, _ access.Subject, r access.Resource) (bool, error) {
				archived, _ := abac.Attr[bool](r.Attrs, "archived")
				return archived, nil
			}),
		abac.Allow("own-document", "documents:*", "document", abac.Owner("owner_id")),
		abac.Allow("public-read", "documents:read", "document",
			func(_ context.Context, _ access.Subject, r access.Resource) (bool, error) {
				public, _ := abac.Attr[bool](r.Attrs, "public")
				return public, nil
			}),
	))
	if err != nil {
		b.Fatal(err)
	}
	return p
}

func BenchmarkDecideAllow(b *testing.B) {
	p := benchPolicy(b)
	ctx := context.Background()
	sub := access.Subject{ID: "u1"}
	res := access.Resource{Type: "document", ID: "d1", Attrs: map[string]any{"owner_id": "u1"}}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.Decide(ctx, sub, "documents:read", res)
	}
}

func BenchmarkDecideDeny(b *testing.B) {
	p := benchPolicy(b)
	ctx := context.Background()
	sub := access.Subject{ID: "u1"}
	res := access.Resource{Type: "document", ID: "d1", Attrs: map[string]any{"owner_id": "u1", "archived": true}}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.Decide(ctx, sub, "documents:write", res)
	}
}

func BenchmarkDecideAbstain(b *testing.B) {
	p := benchPolicy(b)
	ctx := context.Background()
	sub := access.Subject{ID: "u1"}
	res := access.Resource{Type: "report", ID: "r1"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.Decide(ctx, sub, "reports:read", res)
	}
}

// BenchmarkDecideWidePolicy is the worst case for the linear rule scan: 60
// rules across 20 resource types, deciding an action only the last rule
// matches.
func BenchmarkDecideWidePolicy(b *testing.B) {
	rules := make([]abac.Rule, 0, 60)
	for i := range 60 {
		typ := "type" + string(rune('a'+i%20))
		rules = append(rules, abac.Allow(fmt.Sprintf("rule-%d", i), fmt.Sprintf("noun%d:read", i), typ, truePredBench))
	}
	p, err := abac.New(abac.WithRules(rules...))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	sub := access.Subject{ID: "u1"}
	res := access.Resource{Type: "type" + string(rune('a'+59%20))}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.Decide(ctx, sub, "noun59:read", res)
	}
}

func truePredBench(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
	return true, nil
}

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = benchPolicy(b)
	}
}
