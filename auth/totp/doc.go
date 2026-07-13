// Package totp is the complete 2FA package: RFC 6238 TOTP and RFC 4226
// HOTP code generation and verification, otpauth:// provisioning URIs,
// and one-time backup codes — with a Manager that runs the whole
// enrollment/verification lifecycle over a pluggable Store.
//
// Two layers are public. The Manager is the front door: it owns
// pending-enrollment state, seals TOTP secrets with authenticated
// encryption (crypto/secret) before they reach any store, stores backup
// codes only as SHA-256 hashes, and routes replay/race protection through
// two atomic store operations. The stateless primitives underneath
// (Verify, Code, GenerateBackupCodes, ...) are the escape hatch for
// consumers with their own storage discipline.
//
// # Usage
//
// Construction — encryption is mandatory, the key is 32 bytes (use
// ManagerFromKeyset for rotation):
//
//	store := totp.NewMemoryStore() // or pgstore.New(pool), or your own
//	mgr, err := totp.NewManager(store, key, totp.WithIssuer("Acme"))
//
// Enrollment — show the QR, then prove the authenticator works with its
// first code; the returned backup codes exist in plaintext exactly once:
//
//	enr, err := mgr.BeginEnroll(ctx, user.ID, user.Email)
//	img, err := qrcode.DataURI(enr.URI) // core/qrcode renders the otpauth URI
//	// ... user scans and submits their first code ...
//	backupCodes, err := mgr.ConfirmEnroll(ctx, user.ID, firstCode)
//
// Login — one call verifies both TOTP and backup codes, replay-safe:
//
//	res, err := mgr.Verify(ctx, user.ID, form.Code)
//	switch {
//	case errors.Is(err, totp.ErrReplayed), errors.Is(err, totp.ErrInvalidCode):
//		// reject
//	case err != nil:
//		// operator problem (store down, wrong key material)
//	case res.UsedBackupCode && res.BackupRemaining <= 2:
//		// warn: few backup codes left — offer RegenerateBackupCodes
//	}
//
// # Grace period (skip the prompt for a while)
//
// Whether to prompt at all is session policy, so it stays in the consumer.
// User-scoped grace is one line off LastVerified:
//
//	last, err := mgr.LastVerified(ctx, user.ID)
//	if err == nil && time.Since(last) < 12*time.Hour {
//		// skip the OTP prompt
//	}
//
// Caveat: this is user-global — verifying on one device silences the
// prompt on every device for the window. Prefer device-scoped trust.
//
// # Remember this device (recommended)
//
// Device-scoped trust is a signed cookie via crypto/token; the trust
// window is the token TTL and rotation revokes fleet-wide:
//
//	type trusted struct{ UserID string `json:"uid"` }
//	codec, _ := token.New[trusted](key,
//		token.WithTTL(30*24*time.Hour), token.WithPurpose("2fa-device"))
//
//	// after a successful Verify, when the user opts in:
//	tok, _ := codec.Issue(trusted{UserID: user.ID})
//	http.SetCookie(w, &http.Cookie{Name: "2fa_device", Value: tok,
//		MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: true,
//		SameSite: http.SameSiteLaxMode})
//
//	// before showing the OTP prompt:
//	if c, err := r.Cookie("2fa_device"); err == nil {
//		if got, err := codec.Parse(c.Value); err == nil && got.UserID == user.ID {
//			// skip the prompt — this browser verified recently
//		}
//	}
//
// # Multi-tenancy
//
// Multi-tenant applications wire tenant isolation once, at construction,
// with WithScope — never by encoding tenant IDs into subjects:
//
//	mgr, err := totp.NewManager(store, key,
//		totp.WithIssuer("Acme"),
//		totp.WithScope(func(ctx context.Context) (string, error) {
//			return tenantFromContext(ctx) // e.g. tenant middleware
//		}),
//	)
//
// The hook runs inside every operation and fails closed: an error or empty
// scope aborts with ErrScope instead of falling through to the unscoped
// namespace. Pick the model by asking where an enrollment belongs:
//
//   - Single-tenant: omit WithScope.
//   - Account-global 2FA (GitHub model — one enrollment serves every
//     workspace the user belongs to): also omit WithScope; the subject is
//     the global user ID.
//   - Per-tenant enrollment (Slack model — identity lives inside the
//     workspace): wire WithScope to the request's tenant. The same subject
//     enrolls independently per tenant.
//   - Platform-level operations while scoped: use a context bound to the
//     target tenant, or a reserved non-empty sentinel scope (e.g.
//     "@global") that no tenant ID can collide with.
//
// Cleanup: tenant offboarding is DisableTenant under a tenant-bound
// context. Deleting one user across all tenants is the consumer's
// membership loop — for each membership, run Disable with that tenant's
// context; the application owns the membership list, not this package.
//
// # Custom stores
//
// A Store implementation is five CRUD-ish methods plus two atomic gates.
// All correctness lives in the gates:
//
//   - MarkUsed(tenant, subject, usedAt) must atomically set LastUsedAt =
//     usedAt only if the stored value is earlier (or zero), reporting
//     whether it did. Reference SQL:
//     UPDATE forge_totp SET last_used_at=$3 WHERE tenant=$1 AND subject=$2
//     AND (last_used_at IS NULL OR last_used_at < $3)
//   - ConsumeBackup(tenant, subject, hash) must atomically remove hash if
//     present, reporting whether it did. Reference SQL:
//     UPDATE forge_totp SET backup_hashes=array_remove(backup_hashes,$3)
//     WHERE tenant=$1 AND subject=$2 AND $3 = ANY(backup_hashes)
//
// Both report false (not an error) when the condition fails, including for
// absent records. Tenant is compared only by equality; "" is the unscoped
// namespace.
//
// # Rate limiting
//
// This package does not count attempts. Six-digit codes survive online
// brute force only behind a limiter — put auth/lockout (or
// resilience/ratelimit) in front of Verify.
package totp
