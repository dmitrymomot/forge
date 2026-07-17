// Package shortlink creates and resolves short-code links over a
// storage-agnostic Store: collision-retried code generation over the
// unambiguous base58 alphabet, vanity codes with a reserved-word blocklist,
// expiry and deactivation with a configurable fallback, and an OnHit hook
// that emits every successful resolve — counting stays the caller's job.
//
// Destinations live server-side (never in a ?url= query parameter) and are
// scheme-allowlisted at creation, so the redirect endpoint cannot be abused
// as an open redirector. Redirects are 302/307 with Cache-Control: no-store
// — a cached 301 would kill hit counting forever.
//
//	mgr := shortlink.New(shortlink.NewMemoryStore()) // pgstore.New(pool) in production
//
//	link, err := mgr.Create(ctx, shortlink.CreateParams{
//		URL: "https://example.com/docs/getting-started",
//	})
//	// link.Code is e.g. "x7Kp2Wq" → https://sho.rt/x7Kp2Wq
//
//	promo, err := mgr.Create(ctx, shortlink.CreateParams{
//		URL: "https://example.com/summer-sale", Code: "summer",
//	})
//
//	mux.Handle("GET /{code}", mgr.Handler())
//
// Production setups add a cache read-through and a hit hook:
//
//	mgr := shortlink.New(pgstore.New(pool),
//		shortlink.WithCache(redisStore),
//		shortlink.WithOnHit(func(ctx context.Context, l shortlink.Link) {
//			clicks.Push(ctx, l.Code) // enqueue; counting is the caller's job
//		}),
//		shortlink.WithFallbackURL("https://example.com/link-gone"),
//	)
//
// The hook runs synchronously on the redirect hot path — hand the hit to a
// bounded sink (a queue, a buffered channel drained by a worker), don't do
// the counting inline or spawn a goroutine per hit.
//
// Multi-tenant applications confine management operations with WithScope;
// Resolve and the Handler need no hook because a short code is a public
// URL:
//
//	mgr := shortlink.New(store, shortlink.WithScope(func(ctx context.Context) (string, error) {
//		return tenantFromCtx(ctx), nil // fail-closed: empty or error aborts
//	}))
//
// Branded short domains compose with the rest of forge: point a tenant's
// custom domain at the app and mount Handler under web/hostrouter.
//
// Not magiclink (a self-contained signed token); not smartlink (no rules —
// a code resolves to exactly one destination).
package shortlink
