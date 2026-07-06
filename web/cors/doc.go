// Package cors is middleware that answers CORS preflights and annotates
// actual requests with the appropriate Access-Control-* headers.
//
// Requests without an Origin header pass through untouched. Preflights
// (OPTIONS with an Access-Control-Request-Method header) are answered
// directly with a 204 — the wrapped handler never sees them. Actual requests
// from a disallowed origin are still served with no CORS headers attached:
// the browser, not the server, enforces the same-origin restriction, so a
// disallowed request simply fails the browser's CORS check on the response.
//
// AllowedOrigins accepts exact origins, the bare wildcard "*", or a
// single-label wildcard subdomain such as "https://*.example.com" (matches
// "https://a.example.com" but not "https://a.b.example.com" or
// "https://example.com" itself). The bare "*" combined with
// AllowCredentials is rejected at construction — that combination lets any
// site read credentialed responses, which browsers themselves refuse to
// honor and which New treats as a config error via ErrInvalidConfig.
//
// # Usage
//
//	// CORS_ALLOWED_ORIGINS=https://app.example.com,https://*.tenant.example.com
//	mw, err := cors.New(cors.WithConfig(cfg))
//	if err != nil {
//		// invalid origin pattern, or "*" + credentials
//	}
//	handler := middleware.Wrap(mux, mw)
//
// # Dynamic origins
//
// WithOriginFunc replaces AllowedOrigins matching entirely, for origins that
// aren't known at startup — for example tenant custom domains stored in a
// database:
//
//	mw, err := cors.New(cors.WithOriginFunc(func(origin string) bool {
//		return tenants.HasDomain(origin)
//	}))
package cors
