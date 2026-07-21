// Package transport ships the client-credential transports behind
// session.WithTransport: how a session token travels between server and
// client. Any session.Transport implementation plugs into the same seam, so
// custom carriers (query-signed URLs, gRPC metadata bridges) are one small
// struct away.
//
//   - Cookie: HttpOnly/Secure/SameSite cookie — the browser-app default.
//     The safest attributes are on by default; loosen them explicitly.
//
//   - Bearer: "Authorization: Bearer <token>" in, a response header
//     (default X-Session-Token) out — SPAs and mobile clients that store
//     the token themselves.
//
//   - Basic: the token rides the password slot of HTTP Basic auth —
//     curl-friendly APIs and legacy integrations. Extraction-only: the
//     client manages its own credential, so Embed/Clear are no-ops and the
//     consumer delivers the token out of band (response body at login).
//
//   - JWT: the opaque token travels wrapped in a signed JWT (auth/jwt) with
//     exp bound to the session deadline — for edges and gateways that
//     already speak JWT. The JWT is transport dressing: the server-side
//     session stays the source of truth and revocation keeps working.
//
// Wiring and the request-level flow:
//
//	mgr, err := session.New[Data](store,
//		session.WithTransport(transport.Cookie(transport.WithCookieName("sid"))))
//
//	// handlers:
//	s, err := mgr.LoadRequest(r)                       // extract + load
//	err = mgr.SaveRequest(w, r, s)                     // save + Set-Cookie
//	err = mgr.AuthenticateRequest(w, r, s, userID)     // login: rotate + re-set
//	err = mgr.DestroyRequest(w, r, s)                  // logout: revoke + clear
package transport
