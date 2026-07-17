// Package attribution captures marketing touches — utm_* params, ad-platform
// click IDs, affiliate sub-IDs — into a signed cookie and hands them back at
// conversion time. It answers "where did this signup come from" with a
// single stored touch per visitor.
//
// Middleware watches every request: when the query string carries any
// configured param, the set is recorded as a Touch in a signed cookie
// (web/cookie SetSigned — tamper-proof, client-readable). Which visit wins
// is the Policy: LastTouch (default) lets every new campaign visit
// overwrite the stored touch; FirstTouch keeps the original until it
// expires. The attribution window (default 30 days) bounds both the cookie
// lifetime and a server-side timestamp check, so an expired touch never
// counts even if the browser kept the cookie. Requests with no query string
// pass through with zero overhead; an unrelated query string costs one
// parse and no cookie work.
//
// At conversion (signup, purchase), Touch returns the stored touch —
// persist its params with the conversion, then Clear the cookie so the
// same touch is not counted twice. Pixel serves a 1×1 transparent GIF with
// caching disabled, capturing params from the pixel URL for pages the
// middleware can't wrap.
//
// # Non-goals
//
//   - No multi-touch models: exactly one stored touch, first or last.
//   - No server-side touch storage or identity stitching — the cookie is
//     the store; persisting touches against users at conversion is the
//     consumer's insert.
//   - No bot filtering: wrap Pixel or the middleware chain with your own
//     gate if crawler traffic skews numbers.
//
// # Usage
//
//	codec, _ := cookie.New(ks)
//	tracker := attribution.New(codec, attribution.WithExtraParams("ref", "aff_sub"))
//	handler := middleware.Wrap(mux, tracker.Middleware())
//	mux.Handle("GET /pixel.gif", tracker.Pixel())
//
//	// At conversion:
//	if touch, err := tracker.Touch(r); err == nil {
//		saveSignupSource(user, touch.Params)
//		tracker.Clear(w)
//	}
package attribution
