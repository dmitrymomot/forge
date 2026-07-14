package rbac_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/rbac"
)

func benchSet(b *testing.B) *rbac.RoleSet {
	b.Helper()
	rs, err := rbac.NewRoleSet(
		rbac.WithRoles(
			rbac.Role("viewer", "documents:read", "comments:read"),
			rbac.Role("editor", "documents:*"),
			rbac.Role("admin", "*"),
			rbac.Role("auditor", "auditlog:read"),
		),
		rbac.WithRoleInheritance(
			rbac.RoleInherits("editor", "viewer"),
			rbac.RoleInherits("manager", "editor", "auditor"),
		),
	)
	if err != nil {
		b.Fatal(err)
	}
	return rs
}

func BenchmarkCan(b *testing.B) {
	rs := benchSet(b)
	roles := []string{"editor"}
	b.ReportAllocs()
	for b.Loop() {
		_ = rs.Can(roles, "documents:write")
	}
}

func BenchmarkHasRole(b *testing.B) {
	rs := benchSet(b)
	roles := []string{"manager"}
	b.ReportAllocs()
	for b.Loop() {
		_ = rs.HasRole(roles, "viewer")
	}
}

func BenchmarkDeciderFromSubject(b *testing.B) {
	rs := benchSet(b)
	d := rbac.Decider(rs, rbac.FromSubject())
	sub := access.Subject{ID: "u1", Roles: []string{"editor"}}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = d.Decide(ctx, sub, "documents:write", access.Resource{})
	}
}

func BenchmarkResolve(b *testing.B) {
	rs := benchSet(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = rs.Resolve("manager")
	}
}

func BenchmarkNewRoleSet(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = benchSet(b)
	}
}
