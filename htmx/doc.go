// Package htmx provides small, stateless helpers for the HTMX HTTP header
// contract: reading HX-* request headers (IsRequest, IsBoosted, Target,
// TriggerID, ...) and writing HX-* response directives (Redirect, Location,
// Refresh, PushURL, Reswap, Retarget, Reselect, and the Trigger family).
//
// The helpers are free functions — there is no constructor, options object, or
// global state. HTML output is not this package's concern; render the body with
// the render package (or any handler) after setting the htmx headers:
//
//	if htmx.IsRequest(r) {
//		htmx.Retarget(w, "#cart")
//		htmx.Trigger(w, "cart:updated")
//		_ = render.Templ(r.Context(), w, http.StatusOK, views.CartFragment(item))
//		return
//	}
//	_ = render.Templ(r.Context(), w, http.StatusOK, views.CartPage(item))
//
// Most directives only set a response header and must be called before the body
// is written. The redirect helpers (Redirect, Location, LocationWith) are the
// exception: they branch on whether the request came from HTMX — setting the
// HX-Redirect / HX-Location header and a 200 for HTMX requests, or falling back
// to a standard http.Redirect (3xx) for everyone else — and they commit the
// response, so call them last.
//
// When one URL serves both an HTMX partial and a full page, add
// Vary: HX-Request so shared caches do not cross-serve the two variants.
//
// htmx depends only on the standard library; it does not import render.
package htmx
