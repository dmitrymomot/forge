// Package tenant resolves which tenant an inbound HTTP request belongs to
// and carries the tenant ID through context transport-agnostically. It is
// pure transport layer: the package derives a candidate identifier from the
// request, validates it through consumer-owned seams, and yields the
// canonical tenant ID — how that ID scopes storage is each consumer
// package's concern (wire tenant.Scope into their WithScope options).
//
// Resolution is a precedence-ordered chain of Source funcs configured on a
// Resolver, and every source yields the canonical tenant ID (uuid/ulid/short
// id — whatever the consumer's tenants table keys on), never an alias:
// subdomain labels and custom domains are translated through the
// storage-agnostic SubdomainLookup/DomainLookup seams, and Map adds the same
// translation to any other source. Shipped sources cover the common SaaS
// shapes: subdomain against a base domain, custom domain, header, cookie,
// query parameter, path prefix, and a context passthrough for identities
// derived upstream (e.g. from an API key). The first source deriving a
// non-empty ID wins; the optional Validator seam then rejects tenants that
// exist but must not serve traffic (soft-deleted, disabled, suspended).
//
// The carrier is plain context: HTTP middleware stamps it, queue handlers
// and cron jobs call NewContext/FromContext directly — no transport
// assumptions. Scope is the fail-closed read used as the scope hook by every
// other forge package's WithScope option.
//
// Single-tenant apps skip the package entirely; multi-tenant apps opt in per
// route. Nothing is implicit: an unresolved request passes through
// untenanted (add Require where tenancy is mandatory), while a resolved but
// invalid tenant fails closed with a 404.
//
// # Usage
//
//	// Real apps back these seams with their tenants table.
//	domains := tenant.StaticDomains(map[string]string{"shop.acme.com": "01JT9GA6Z3"})
//	subdomains := tenant.StaticSubdomains(map[string]string{"acme": "01JT9GA6Z3"})
//
//	resolver := tenant.New(
//		tenant.WithSources(
//			tenant.Domain(domains),                          // custom domains win,
//			tenant.Subdomain("app.example.com", subdomains), // then acme.app.example.com
//		),
//		tenant.WithValidator(tenant.ValidatorFunc(func(ctx context.Context, id string) error {
//			return nil // consult your tenants table: ErrTenantNotFound / ErrTenantInactive
//		})),
//	)
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
