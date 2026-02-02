// Package hostrouter provides host-based HTTP routing.
//
// It routes incoming requests to different handlers based on the Host header,
// supporting both exact matches and wildcard patterns.
//
// # Host Patterns
//
// Two pattern types are supported:
//
//   - Exact: "api.example.com" matches only that host
//   - Wildcard: "*.example.com" matches any subdomain (foo.example.com, bar.example.com)
//
// Exact matches take priority over wildcard matches. Host matching is case-insensitive,
// and ports are stripped before matching.
//
// # Usage
//
//	routes := hostrouter.Routes{
//	    "api.example.com":   apiHandler,
//	    "*.example.com":     wildcardHandler,
//	}
//	router := hostrouter.New(routes, defaultHandler)
//	http.ListenAndServe(":8080", router)
//
// # IPv6 Support
//
// IPv6 addresses are supported. The router correctly handles addresses with ports
// like "[::1]:8080" by preserving the brackets during normalization.
package hostrouter
