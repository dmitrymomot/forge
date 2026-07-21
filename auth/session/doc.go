// Package session drives the server-side session lifecycle —
// Start/Load/Save/Destroy/Rotate — over a pluggable Store, with typed
// session data, rotate-on-privilege-change, multi-device management
// (list/revoke/logout-others with UI-ready metadata), pluggable client
// transports, and optional fingerprint-based hijack detection.
//
// The Manager is generic over the consumer's payload type (JSON-encoded at
// rest) and deals in bearer tokens; the request-level methods ride a
// pluggable Transport (session/transport: Cookie, Bearer, Basic, JWT, or
// any custom implementation). Sessions expire on an idle TTL slid by each
// Save, capped by an absolute lifetime no activity extends.
//
//	type Data struct {
//		Cart []string `json:"cart,omitempty"`
//	}
//
//	mgr, err := session.New[Data](session.NewMemoryStore(), // pgstore/cookiestore/NewKVStore in production
//		session.WithTransport(transport.Cookie()))
//	if err != nil { ... }
//
//	// First visit: start, mutate, save — SaveRequest sets the cookie and
//	// stamps IP/User-Agent/LastSeenAt for the device listing.
//	s := mgr.Start(r.Context())
//	s.Data.Cart = append(s.Data.Cart, "sku-1")
//	if err := mgr.SaveRequest(w, r, s); err != nil { ... }
//
//	// Later requests.
//	s, err = mgr.LoadRequest(r)
//	switch {
//	case errors.Is(err, session.ErrNotFound), errors.Is(err, session.ErrExpired):
//		// no session — treat as signed out
//	case err != nil: ...
//	}
//
//	// Login: binds the user and rotates the token so a pre-login token
//	// planted by an attacker (session fixation) dies; the transport
//	// re-embeds the replacement automatically.
//	if err := mgr.AuthenticateRequest(w, r, s, userID); err != nil { ... }
//
//	// Logout: revokes the session and clears the client credential.
//	if err := mgr.DestroyRequest(w, r, s); err != nil { ... }
//
// The token-level API (Load/Save/Authenticate/Rotate/Destroy with a
// context) remains fully usable without any transport — for non-HTTP
// callers or hand-rolled wiring.
//
// # Stores
//
// The built-in MemoryStore is for tests and development. Production
// backings: pgstore (Postgres, the only driver with UserIndex — device
// listings, per-device revocation, "log out everywhere", GDPR deletion),
// cookiestore (stateless encrypted cookie, no server state, no revocation),
// and NewKVStore over any durable cache.Store (cache/redis).
//
// # Device management (UserIndex stores only)
//
// Sessions carry the metadata a "manage devices" page renders — ID,
// IP, UserAgent, CreatedAt, LastSeenAt, ExpiresAt — and the current device
// is the one whose ID matches the caller's own session:
//
//	devices, err := mgr.ListUserSessions(ctx, userID)      // newest first, Token empty
//	err = mgr.RevokeUserSession(ctx, userID, devices[1].ID) // revoke one device
//	err = mgr.LogoutOthers(ctx, current)                    // every device but this one
//	err = mgr.DeleteUserSessions(ctx, userID)               // log out everywhere / GDPR
//
// # Hijack detection
//
// WithFingerprint(session.Warn|session.Strict) compares each Load against
// the fingerprint captured at Start/Rotate (from web/fingerprint.Middleware
// via context, or a custom WithDigestSource). Warn logs the drifted
// components; Strict revokes the session and returns
// ErrFingerprintMismatch.
//
// # Tenancy
//
// Single-tenant apps pay no ceremony. Multi-tenant apps install a
// construction-time scope hook; sessions are stamped at Save and invisible
// outside their scope, and resolution fails closed when the hook errors or
// returns empty:
//
//	mgr, err := session.New[Data](store, session.WithScope(func(ctx context.Context) (string, error) {
//		t, ok := tenant.FromContext(ctx) // web/tenant
//		if !ok {
//			return "", errors.New("no tenant in context")
//		}
//		return t.ID, nil
//	}))
package session
