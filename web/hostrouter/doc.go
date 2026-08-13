// Package hostrouter routes HTTP requests to different handlers by the request's
// Host header. It supports exact hosts, single-label wildcard subdomains, and a
// lookup seam for customer domains resolved at request time, with a configurable
// fallback, and exposes the matched host/pattern/subdomain to handlers via the
// request context.
//
// A Router is a plain http.Handler, so it composes with httpserver (or any server)
// directly. Matching runs in four steps: exact host, then a single leading label
// against a "*." wildcard ("*.example.com" matches "foo.example.com" but not
// "a.b.example.com" nor the apex "example.com"), then WithLookup, then the fallback.
// Misconfiguration (nil handler, malformed or duplicate pattern, nil lookup) is
// reported by New as a joined, errors.Is-matchable error.
//
// # Security: default-deny is DNS-rebinding protection
//
// Unmatched hosts fall through to the fallback (http.NotFoundHandler(), 404, by
// default). This default-deny is a DNS-rebinding defense: a handler is reachable
// only for explicitly registered Host values, so an attacker who points their own
// domain at your IP reaches the fallback, not a real handler. Do not install a
// WithFallback that serves sensitive handlers without validating the Host itself.
//
// # Customer domains
//
// WithLookup resolves a host no pattern matches, which is how a customer's own
// domain reaches its handler. It runs after the static patterns, so a customer
// domain can never shadow a platform host. It fails closed: a lookup that returns
// an error other than ErrHostNotFound reaches WithLookupErrorHandler (503 by
// default) instead of the fallback, because a store that is down must not read as
// "unknown host" and 404 every customer domain off the platform.
//
// # Usage
//
//	router, err := hostrouter.New(
//		hostrouter.WithHost("api.example.com", apiMux),
//		hostrouter.WithHost("*.example.com", tenantMux),
//		hostrouter.WithLookup(customDomains.Handler),
//		hostrouter.WithFallback(marketingSite),
//	)
//	if err != nil {
//		return err
//	}
//	srv := httpserver.New(router, httpserver.WithAddr(":8080"))
//
//	// Inside a wildcard handler, read the matched subdomain:
//	tenant := hostrouter.Subdomain(r.Context()) // "foo" for foo.example.com
package hostrouter
