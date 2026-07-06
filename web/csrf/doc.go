// Package csrf is stateless double-submit CSRF middleware over a signed
// cookie codec (web/cookie).
//
// On any request, the middleware ensures a signed token cookie exists —
// minting and setting one on first sight — and exposes the token to the
// handler via Token(r). Unsafe methods (POST, PUT, PATCH, DELETE) must echo
// that same token back via a header or form field; the cookie alone is never
// sufficient, since browsers attach cookies automatically on cross-site
// requests too. A request that lacks a valid cookie, an echo, or where the
// two don't match, is rejected before it reaches the handler.
//
// # htmx recipe
//
// Render the token once per page and let htmx forward it on every
// subsequent request:
//
//	<meta name="csrf-token" content="{{ csrf.Token(r) }}">
//	<body hx-headers='{"X-CSRF-Token": "{{ csrf.Token(r) }}"}'>
//
// # Non-goals
//
//   - No per-request token rotation: the token is stable for the life of the
//     cookie, which keeps multi-tab and back-button navigation working.
//   - No Origin/Referer fallback: the double-submit check is the only
//     defense; add an Origin check separately if you need defense in depth.
//   - No session binding: the token is not tied to a user session. Rotate on
//     login by deleting the cookie (codec.Delete) so a fresh token is minted
//     for the new session.
//
// # Usage
//
//	codec, _ := cookie.New(ks)
//	mw := csrf.New(codec)
//	handler := middleware.Wrap(mux, mw)
package csrf
