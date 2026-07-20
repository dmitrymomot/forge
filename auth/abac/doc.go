// Package abac evaluates attribute/relationship predicates — registered Go
// functions — as one layer in the auth/access decision seam: "agent sees own
// subtree but not subagents' player details". There is no policy DSL and no
// storage: the relationship data (trees, assignments, ownership) stays in
// consumer code feeding the predicate, and rules are plain Go registered at
// startup.
//
// A Policy is an immutable, concurrent-safe set of named rules built by New.
// Each rule binds a Predicate to the action pattern (exact "documents:read",
// noun wildcard "documents:*", or super "*") and resource type (exact or "*")
// it governs. Deciding evaluates deny rules before allow rules — a satisfied
// deny vetoes regardless of registration order — and abstains when nothing
// matched, so rbac/acl layers below can speak. A predicate error fails the
// decision closed through the seam.
//
// Tenancy is a passed value: predicates receive Subject.Tenant and
// Resource.Tenant on their arguments, and access.TenantMatch placed first in
// the chain vetoes cross-tenant access by construction. Single-tenant apps
// leave both empty.
//
// Wiring (composed with rbac under the documented precedence):
//
//	policy, err := abac.New(abac.WithRules(
//		abac.Allow("own-document", "documents:*", "document", abac.Owner("owner_id")),
//		abac.Deny("archived-write", "documents:write", "document",
//			func(ctx context.Context, s access.Subject, r access.Resource) (bool, error) {
//				archived, _ := abac.Attr[bool](r.Attrs, "archived")
//				return archived, nil
//			}),
//	))
//	if err != nil { ... }
//	decider := access.FirstDecisive(access.TenantMatch(), policy, rbacDecider)
//	mux.Handle("PUT /docs/{id}", authn(docs.Handle(decider, "documents:write", updateDoc)))
package abac
