// Package otp issues and verifies short numeric one-time codes for email and
// SMS verification and passwordless login. Codes are attempt-limited, TTL'd,
// single-use, and stored only as keyed hashes: a store-only compromise
// reveals nothing without the application's secret, because the 10^6 space
// of a 6-digit code makes any unkeyed hash reversible in milliseconds.
//
// Delivery is the caller's channel; anti-enumeration responses and request
// throttling stay in the caller's handlers (compose auth/lockout and
// resilience/ratelimit). TOTP/HOTP authenticator apps are auth/totp.
//
// Create one instance per flow — the purpose isolates codes so a login code
// can never verify a password reset:
//
//	secret := []byte(os.Getenv("OTP_SECRET")) // min 32 bytes
//	store := cache.NewMemoryStore()           // dev/tests; bring a durable cache.Store in production
//
//	loginOTP, err := otp.New(secret, store, otp.WithPurpose("login"))
//	if err != nil { ... }
//
//	// Request a code. Deliver over your channel; reply 202 whether or not
//	// the account exists.
//	code, err := loginOTP.Generate(ctx, email)
//
//	// Verify. Map ErrNotFound and ErrCodeMismatch to ONE user-facing
//	// message so responses reveal nothing about code existence.
//	err = loginOTP.Verify(ctx, email, submitted)
//	switch {
//	case err == nil:
//		// authenticated
//	case errors.Is(err, otp.ErrTooManyAttempts):
//		// "too many attempts — request a new code"
//	default:
//		// "invalid or expired code"
//	}
//
// The identifier is an opaque string the package never interprets.
// Canonicalize it (lowercase and trim emails, E.164 phone numbers) in ONE
// helper used by both Generate and Verify — different forms derive different
// storage keys and the code will never match. Calling Generate again for the
// same identifier replaces the outstanding code ("resend"); Revoke cancels
// it.
//
// # Multi-tenancy
//
// Multi-tenant applications wire tenant isolation once, at construction,
// with WithScope — never by concatenating tenant IDs into identifiers at
// call sites:
//
//	loginOTP, err := otp.New(secret, store,
//		otp.WithPurpose("login"),
//		otp.WithScope(func(ctx context.Context) (string, error) {
//			return tenantFromContext(ctx) // e.g. data/tenant middleware
//		}),
//	)
//
// The hook runs inside every Generate, Verify, and Revoke, so no call site
// can forget it. It fails closed: an error or empty scope aborts with
// ErrScope instead of falling through to a shared bucket. Single-tenant
// applications omit the option.
//
// The hook is arbitrary — it maps the request context to a scope string — so
// one construction serves every tenancy shape:
//
//   - Single-tenant: omit WithScope; every identifier shares one namespace.
//   - White-label (tenant-locked): return the tenant the request is bound to.
//     A sibling tenant with the same identifier gets a different code, and a
//     request that arrives without a tenant fails closed.
//   - Global user switching tenants: return the active tenant while the user is
//     inside one, and a reserved, non-empty global sentinel (e.g. "@global") at
//     the platform level. "Switching" is just a different context per request;
//     each scope is an isolated bucket.
//
// Because the hook fails closed on an empty string, the global case must return
// a non-empty sentinel, not "". Keep that sentinel disjoint from real tenant
// IDs so a global user and a tenant can never collide in the same bucket.
//
// # Storage
//
// State rides the resilience/cache.Store seam. Keys carry no PII (scope and
// identifier are hashed with length-prefixed domain separation); values are
// 42-byte records holding a format-version byte, the attempt counter, expiry,
// and the HMAC-SHA256(secret, code) digest. The in-memory store's LRU eviction
// makes it unsuitable for production codes — supply your own durable
// Store. A failed attempt rewrites the record with its remaining TTL, so
// retries never extend a code's life. The attempt counter is
// read-modify-write: concurrent wrong guesses can overshoot the limit by the
// in-flight count, which is immaterial against a million-value keyspace;
// per-identifier lockout is auth/lockout's layer. Single-use is enforced per
// completed verification, not as a mutual exclusion: concurrent submissions of
// the correct code may each succeed before either deletes the record — harmless,
// since that requires already holding the valid code.
package otp
