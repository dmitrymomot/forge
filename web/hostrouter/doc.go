// Package hostrouter routes HTTP requests to different handlers by the request's
// Host header. It supports exact hosts and single-label wildcard subdomains, with a
// configurable fallback, and exposes the matched host/pattern/subdomain to handlers
// via the request context.
//
// A Router is a plain http.Handler, so it composes with httpserver (or any server)
// directly. Matching is exact first, then a single leading label against a "*."
// wildcard: "*.example.com" matches "foo.example.com" but not "a.b.example.com" nor
// the apex "example.com". Misconfiguration (nil handler, malformed or duplicate
// pattern) panics at construction with an errors.Is-matchable sentinel.
//
// # Security: default-deny is DNS-rebinding protection
//
// Unmatched hosts fall through to the fallback (http.NotFoundHandler(), 404, by
// default). This default-deny is a DNS-rebinding defense: a handler is reachable
// only for explicitly registered Host values, so an attacker who points their own
// domain at your IP reaches the fallback, not a real handler. Do not install a
// WithFallback that serves sensitive handlers without validating the Host itself.
//
// # Usage
//
//	router := hostrouter.New(
//		hostrouter.WithHost("api.example.com", apiMux),
//		hostrouter.WithHost("*.example.com", tenantMux),
//		hostrouter.WithFallback(marketingSite),
//	)
//	srv := httpserver.New(router, httpserver.WithAddr(":8080"))
//
//	// Inside a wildcard handler, read the matched subdomain:
//	tenant := hostrouter.Subdomain(r.Context()) // "foo" for foo.example.com
package hostrouter
