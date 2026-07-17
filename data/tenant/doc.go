// Package tenant is forge's multi-tenancy package: it resolves which tenant
// an inbound request belongs to, carries the tenant ID through context
// transport-agnostically, and scopes SQL queries with explicit parameterized
// fragments — visible at every query, never auto-injected.
//
// Resolution is a precedence-ordered chain of Resolver funcs. Shipped
// resolvers cover the common SaaS shapes: subdomain against a base domain,
// custom domain through the storage-agnostic DomainLookup seam, header,
// cookie, path prefix, and a context passthrough for identities derived
// upstream (e.g. from an API key). The first resolver returning a non-empty
// ID wins.
//
// The carrier is plain context: HTTP middleware stamps it, queue handlers
// and cron jobs call NewContext/FromContext directly — no transport
// assumptions. Scope is the fail-closed read used as the scope hook by every
// other forge package's WithScope option.
//
// Single-tenant apps skip the package entirely; multi-tenant apps opt in per
// route and per query. Nothing is implicit: an unresolved request passes
// through untenanted (add Require where tenancy is mandatory), and a query
// is scoped only where a ScopeClause is visibly concatenated.
//
// # Usage
//
//	lookup := tenant.StaticDomains(map[string]string{ // real apps: DB-backed DomainLookup
//		"shop.acme.com": "acme",
//	})
//
//	mux := http.NewServeMux()
//	mux.Handle("/orders", tenant.Require(http.HandlerFunc(listOrders)))
//
//	handler := tenant.Middleware(
//		tenant.Domain(lookup),                // custom domains win,
//		tenant.Subdomain("app.example.com"),  // then acme.app.example.com
//	)(mux)
//
//	// In any handler or job:
//	func listOrders(w http.ResponseWriter, r *http.Request) {
//		c, err := tenant.ScopeClause(r.Context(), "tenant_id", "$1")
//		if err != nil { /* 404/500 — never fall back unscoped */ }
//		rows, err := db.Query(r.Context(), "SELECT id FROM orders WHERE "+c.SQL, c.Arg)
//		// ...
//	}
//
// Queue handlers carry tenancy the same way: the producer stamps the job's
// context (or payload), the consumer restores it with NewContext, and
// ScopeClause works unchanged.
package tenant
