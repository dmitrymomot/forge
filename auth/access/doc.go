// Package access is the authorization decision seam — it answers "can this
// subject do this action on this resource?" and is the 403 half of guard's
// 401-vs-403 split (guard authenticates → 401; access authorizes → 403).
//
// The seam is a three-valued Decider: every layer returns Allow, Deny, or
// Abstain ("no opinion"), so layers compose under an explicit precedence.
// FirstDecisive takes the first Allow/Deny and lets Abstain fall through;
// DenyOverrides lets any Deny veto. rbac, acl, and abac will each implement
// Decider and drop into a chain — access never imports them, owns no storage,
// and never fetches a resource; resource attributes are caller-supplied.
//
// Built-in deciders cover the standalone case: ScopeDecider authorizes from
// token scopes, TenantMatch vetoes cross-tenant access, and AllowAll/DenyAll
// are terminals. Every Decision carries an explanation record (which layer
// decided and why) for auditlog and the "why can't this user do X" ticket.
//
// The RequirePermission middleware gates a route on a static action; the
// generic Model[T] handler loads an object, authorizes, and hands the loaded
// object to the business handler — collapsing per-handler ownership boilerplate.
//
// Wiring (behind a guard authn middleware):
//
//	decider := access.FirstDecisive(
//		access.TenantMatch(),
//		access.ScopeDecider(),
//	)
//	read := access.RequirePermission(decider, "documents:read",
//		access.WithResource(func(r *http.Request) access.Resource {
//			return access.Resource{Type: "document", ID: r.PathValue("id"), Tenant: r.PathValue("tenant")}
//		}),
//	)
//	mux.Handle("GET /t/{tenant}/docs/{id}", authn(read(getDoc)))
//
// Resource ownership decided in-handler after the load:
//
//	var docs = access.NewModel(
//		func(r *http.Request) (Document, error) { return store.Load(r.Context(), r.PathValue("id")) },
//		func(d Document) access.Resource {
//			return access.Resource{Type: "document", ID: d.ID, Tenant: d.Tenant, Attrs: map[string]any{"owner_id": d.OwnerID}}
//		},
//	)
//	mux.Handle("PUT /docs/{id}", authn(docs.Handle(editDecider, "documents:write",
//		func(w http.ResponseWriter, r *http.Request, d Document) { /* authorized; d loaded */ })))
package access
