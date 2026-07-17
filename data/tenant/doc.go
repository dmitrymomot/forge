// Package tenant is forge's multi-tenancy package: it resolves which tenant
// an inbound request belongs to, carries the tenant ID through context
// transport-agnostically, and scopes SQL queries with explicit parameterized
// fragments — visible at every query, never auto-injected.
//
// Resolution is a precedence-ordered chain of Resolver funcs, and every
// resolver yields the canonical tenant ID (uuid/ulid/short id — whatever
// the consumer's tenants table keys on), never an alias: subdomain labels
// and custom domains are translated through the storage-agnostic
// SubdomainLookup/DomainLookup seams, and Map adds the same translation to
// any other resolver. Shipped resolvers cover the common SaaS shapes:
// subdomain against a base domain, custom domain, header, cookie, path
// prefix, and a context passthrough for identities derived upstream (e.g.
// from an API key). The first resolver returning a non-empty ID wins.
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
//	// Real apps back these lookups with their tenants table.
//	domains := tenant.StaticDomains(map[string]string{"shop.acme.com": "01JT9GA6Z3"})
//	subdomains := tenant.StaticSubdomains(map[string]string{"acme": "01JT9GA6Z3"})
//
//	mux := http.NewServeMux()
//	mux.Handle("/orders", tenant.Require(http.HandlerFunc(listOrders)))
//
//	handler := tenant.Middleware(
//		tenant.Domain(domains),                           // custom domains win,
//		tenant.Subdomain("app.example.com", subdomains),  // then acme.app.example.com
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
