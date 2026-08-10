// Package acl is the runtime-data override layer of authorization: explicit
// per-subject, per-resource grant and deny entries — "this manager sees
// exactly these assigned agents" — layered onto rbac's role decisions, with
// deny winning. Entries live behind a storage-agnostic Store (in-memory built
// in; consumers supply a durable driver), administered through a Manager
// scoped by an optional WithScope tenancy hook that fails closed.
//
// The Decider plugs into the auth/access seam: a matching deny entry → Deny,
// a matching grant → Allow, otherwise Abstain — so placed before rbac in a
// chain an ACL deny vetoes what a role allows, and an ACL grant opens exactly
// the listed resources without widening the role. An entry with ResourceID ""
// is type-wide (every resource of the type, collection checks included); an
// entry with Action "*" covers every action on the resource. Grant and deny
// share one key (subject, resource, action) — writing one overwrites the
// other, so contradictory pairs cannot exist.
//
// Wiring (see access for the middleware surface):
//
//	store := acl.NewMemoryStore()
//	admin := acl.NewManager(store)
//	_ = admin.Grant(ctx, "mgr-7", "agent", "42", "*")            // full access to agent 42
//	_ = admin.Deny(ctx, "mgr-7", "report", "", "reports:export") // no export on any report
//
//	decider := access.FirstDecisive(
//		access.TenantMatch(),
//		acl.Decider(store),
//		rbac.Decider(rs, rbac.FromSubject()),
//	)
package acl
