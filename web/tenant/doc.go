// Package tenant resolves which tenant an inbound HTTP request belongs to
// and carries the tenant ID through context transport-agnostically. It is
// pure transport layer, split in two like auth/guard: Sources extract a
// candidate identifier from the request (pure, no I/O), and the single
// consumer-owned Lookup seam translates it to a live canonical tenant ID —
// existence and status (soft-deleted, disabled) are one call, typically one
// query. How the ID scopes storage is each consumer package's concern (wire
// tenant.Scope into their WithScope options).
//
// Shipped sources cover the common SaaS shapes — subdomain against a base
// domain, custom domain, header, cookie, query parameter, path prefix, and
// a context passthrough for identities derived upstream (e.g. from an API
// key) — each tagging its value with a Kind (subdomain label, full domain,
// path segment, canonical ID) so one Lookup implementation can interpret
// every namespace; any func matching the Source signature adds a custom
// source with its own Kind. Sources run in precedence order: the first
// extracted identifier the Lookup resolves wins, ErrTenantNotFound moves to
// the next source, ErrTenantInactive and infrastructure errors fail closed.
//
// The carrier is plain context: HTTP middleware stamps it, queue handlers
// and cron jobs call NewContext/FromContext directly — no transport
// assumptions. Scope is the fail-closed read used as the scope hook by every
// other forge package's WithScope option.
//
// Single-tenant apps skip the package entirely; multi-tenant apps opt in per
// route. Nothing is implicit: an unresolved request passes through
// untenanted (add Require where tenancy is mandatory), while an inactive
// tenant fails closed with a 404.
//
// # Usage
//
//	// One seam answers "which tenant" and "may it serve" — usually one query
//	// per request against the consumer's tenants table.
//	lookup := tenant.LookupFunc(func(ctx context.Context, ident tenant.Identifier) (string, error) {
//		switch ident.Kind {
//		case tenant.KindDomain: // SELECT tenant_id FROM tenant_domains WHERE domain=$1 AND ...
//		case tenant.KindSubdomain: // SELECT id FROM tenants WHERE subdomain=$1 AND deleted_at IS NULL
//		case tenant.KindID: // SELECT id FROM tenants WHERE id=$1 AND deleted_at IS NULL
//		}
//		return "", tenant.ErrTenantNotFound
//	})
//
//	resolver := tenant.New(lookup, tenant.WithSources(
//		tenant.Domain(),                     // custom domains win,
//		tenant.Subdomain("app.example.com"), // then acme.app.example.com
//	))
//
//	mux := http.NewServeMux()
//	mux.Handle("/orders", tenant.Require(http.HandlerFunc(listOrders)))
//	handler := resolver.Middleware()(mux)
//
//	// In any handler or job:
//	func listOrders(w http.ResponseWriter, r *http.Request) {
//		id, err := tenant.Scope(r.Context())
//		if err != nil { /* fail closed — never fall back to an unscoped view */ }
//		_ = id
//	}
//
// Queue handlers carry tenancy the same way: the producer stamps the job's
// context (or payload), the consumer restores it with NewContext, and Scope
// works unchanged.
package tenant
