// Package apikey issues, manages, and verifies API keys: Stripe-style
// prefixed secrets (sk_live_x7Kp…) with a CRC32 checksum that rejects
// malformed credentials before any storage access, SHA-256 hashes at rest,
// and the plaintext returned exactly once at creation.
//
// The package is stateless and ships no storage of any kind. Every
// operation is a free function over a validated Config plus the storage
// effects it performs — SaveFunc, LoadFunc, LoadByHashFunc, ListFunc,
// RevokeFunc, TouchFunc, SwapFunc. The caller supplies each effect as a
// closure at the call site, so a write can ride the caller's transaction
// and carry columns this package never sees. The effects are distinct
// named types even where signatures match, so the compiler rejects a
// swapped argument.
//
// Personal and tenant-wide keys share one model: Subject is the principal
// the key acts as — a user id for personal keys, or a tenant or
// service-account id for keys owned by the org itself — and Tenant
// optionally pins the owning org.
//
// Build the Config once at wiring; it holds no storage and no mutable
// state, so one value serves every goroutine:
//
//	cfg, err := apikey.NewConfig(apikey.WithPrefix("sk_live"))
//
// A write closes over the request's transaction and repository:
//
//	err := pg.InTx(ctx, func(tx pgx.Tx) error {
//		q := repo.WithTx(tx)
//		key, plaintext, err := apikey.Create(ctx, cfg, apikey.CreateParams{
//			Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
//			Scopes:  []string{"deploy:write"},
//		}, func(ctx context.Context, k apikey.Key) error {
//			return q.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
//				ID: k.ID, Hash: k.Hash, Preview: k.Preview,
//				Subject: k.Subject, TenantID: tenantID, // columns apikey never defines
//			})
//		})
//		...
//	})
//
// Create returns the plaintext wrapped in redact.Secret: it renders as
// "REDACTED" through fmt, encoding/json, and log/slog, so it cannot reach
// a log line or an error body by accident. Expose is the only way to read
// it, and the caller shows it exactly once:
//
//	render(w, plaintext.Expose())
//
// Afterwards only key.Preview ("sk_live_x7Kp") remains for dashboards, and
// only the SHA-256 of the plaintext is stored.
//
// Middleware wiring curries the same logic into the guard.Verifier seam:
//
//	verifier, err := apikey.NewVerifier(cfg, repo.GetAPIKeyByHash, repo.TouchAPIKey)
//	authn := guard.New(verifier, guard.WithExtractors(guard.BearerHeader(), guard.Header("X-API-Key")))
//	mux.Handle("POST /api/deploy", authn(deployHandler))
//
// Tests need no fixture from this package: an effect is a closure, so a
// test states the one behaviour it needs inline, and a test that needs
// state across calls brings its own map.
//
//	_, _, err := apikey.Create(ctx, cfg, params,
//		func(context.Context, apikey.Key) error { return apikey.ErrDuplicate })
//
// Scopes are carried into guard.Identity.Scopes but never enforced here —
// enforcement belongs to the authorization seam (auth/rbac).
//
// Multi-tenant applications confine management operations with WithScope;
// verification needs no hook because the key record itself resolves the
// tenant:
//
//	cfg, err := apikey.NewConfig(apikey.WithScope(func(ctx context.Context) (string, error) {
//		return tenantFromCtx(ctx), nil // fail-closed: empty or error aborts
//	}))
//
// Rotation overlaps old and new keys so consumers can deploy the new
// plaintext before the old one dies. SwapFunc performs both writes as one
// transaction, so a failed rotation leaves no orphan replacement:
//
//	fresh, plaintext, err := apikey.Rotate(ctx, cfg, key.ID, 24*time.Hour, repo.GetAPIKey, repo.RotateAPIKey)
package apikey
