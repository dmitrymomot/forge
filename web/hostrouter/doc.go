// Package hostrouter routes HTTP requests to different handlers by the request's
// Host header. It supports exact hosts and single-label wildcard subdomains, with a
// configurable fallback, and exposes the matched host/pattern/subdomain to handlers
// via the request context.
//
// A Router is a plain http.Handler, so it composes with httpserver (or any server)
// directly:
//
//	router := hostrouter.New(
//		hostrouter.WithHost("api.example.com", apiMux),
//		hostrouter.WithHost("*.example.com", tenantMux),
//		hostrouter.WithFallback(marketingSite),
//	)
//	srv := httpserver.New(router, httpserver.WithAddr(":8080"))
//
// Matching is exact first, then a single leading label against a "*." wildcard:
// "*.example.com" matches "foo.example.com" but not "a.b.example.com" nor the apex
// "example.com". Misconfiguration (nil handler, malformed or duplicate pattern)
// panics at construction with an errors.Is-matchable sentinel.
//
// Inside a wildcard handler, read the matched subdomain:
//
//	tenant := hostrouter.Subdomain(r.Context()) // "foo" for foo.example.com
package hostrouter
