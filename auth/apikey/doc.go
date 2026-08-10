// Package apikey issues, manages, and verifies API keys: Stripe-style
// prefixed secrets (sk_live_x7Kp…) with a CRC32 checksum that rejects
// malformed credentials before any store access, SHA-256 hashes at rest,
// and the plaintext returned exactly once at creation. Management —
// create/list/revoke/rotate, per-key scopes, optional expiry, throttled
// last-used-at — runs behind the storage-agnostic Store seam;
// verification implements guard.Verifier.
//
// Personal and tenant-wide keys share one model: Subject is the principal
// the key acts as — a user id for personal keys, or a tenant or
// service-account id for keys owned by the org itself — and Tenant
// optionally pins the owning org.
//
//	store := apikey.NewMemoryStore() // bring a durable Store in production
//	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))
//
//	key, plaintext, err := mgr.Create(ctx, apikey.CreateParams{
//		Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
//		Scopes:  []string{"deploy:write"},
//	})
//	// Show plaintext once; key.Preview ("sk_live_x7Kp") is what
//	// dashboards keep. Only the SHA-256 of the plaintext is stored.
//
//	authn := guard.New(mgr, guard.WithExtractors(guard.BearerHeader(), guard.Header("X-API-Key")))
//	mux.Handle("POST /api/deploy", authn(deployHandler))
//
// Scopes are carried into guard.Identity.Scopes but never enforced here —
// enforcement belongs to the authorization seam (auth/rbac).
//
// Multi-tenant applications confine management operations with WithScope;
// verification needs no hook because the key record itself resolves the
// tenant:
//
//	mgr := apikey.New(store, apikey.WithScope(func(ctx context.Context) (string, error) {
//		return tenantFromCtx(ctx), nil // fail-closed: empty or error aborts
//	}))
//
// Rotation overlaps old and new keys so consumers can deploy the new
// plaintext before the old one dies:
//
//	fresh, plaintext, err := mgr.Rotate(ctx, key.ID, 24*time.Hour)
package apikey
