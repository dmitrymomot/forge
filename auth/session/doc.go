// Package session drives the server-side session lifecycle —
// Start/Load/Save/Destroy/Rotate — over a pluggable Store, with typed
// session data, rotate-on-privilege-change, optional multi-device management,
// and optional fingerprint-based hijack detection.
//
// The Manager is generic over the consumer's payload type (JSON-encoded at
// rest) and transport-agnostic: it deals in bearer tokens, and how a token
// reaches the client (a cookie via web/cookie, a header) is the handler's
// choice. Sessions expire on an idle TTL slid by each Save, capped by an
// absolute lifetime no activity extends.
//
//	type Data struct {
//		Cart []string `json:"cart,omitempty"`
//	}
//
//	mgr, err := session.New[Data](session.NewMemoryStore()) // pgstore/cookiestore/NewKVStore in production
//	if err != nil { ... }
//
//	// First visit: start, mutate, save, hand the token to the client.
//	s := mgr.Start(ctx)
//	s.Data.Cart = append(s.Data.Cart, "sku-1")
//	if err := mgr.Save(ctx, s); err != nil { ... }
//	http.SetCookie(w, &http.Cookie{Name: "sid", Value: s.Token, HttpOnly: true, Secure: true, Path: "/"})
//
//	// Later requests: load by the presented token.
//	s, err = mgr.Load(ctx, cookieValue)
//	switch {
//	case errors.Is(err, session.ErrNotFound), errors.Is(err, session.ErrExpired):
//		// no session — treat as signed out
//	case err != nil: ...
//	}
//
//	// Login: Authenticate binds the user and rotates the token so a
//	// pre-login token planted by an attacker (session fixation) dies.
//	if err := mgr.Authenticate(ctx, s, userID); err != nil { ... }
//	http.SetCookie(w, ...) // s.Token changed — re-set the cookie
//
//	// Logout.
//	if err := mgr.Destroy(ctx, s); err != nil { ... }
//
// # Stores
//
// The built-in MemoryStore is for tests and development. Production
// backings: pgstore (Postgres, the only driver with UserIndex — multi-device
// listings, "log out everywhere", GDPR deletion), cookiestore (stateless
// encrypted cookie, no server state, no revocation), and NewKVStore over any
// durable cache.Store (cache/redis).
//
// Multi-device management (UserIndex stores only):
//
//	devices, err := mgr.ListUserSessions(ctx, userID) // newest first, Token empty
//	err = mgr.DeleteUserSessions(ctx, userID)         // log out everywhere
//	err = mgr.Save(ctx, current)                      // then keep this device: re-persist it
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
