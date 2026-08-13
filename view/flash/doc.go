// Package flash carries one-shot messages across a redirect.
//
// A write that ends in a redirect has nowhere to say what it did: the response that
// knows is the one the browser throws away. Set stages the message on that response,
// Take reads it once on the next request and clears it, so a reload shows nothing.
// This is the missing half of post/redirect/get.
//
// # Stores
//
// CookieStore keeps the messages in one signed cookie and needs no backing store,
// which makes it the default. The payload travels to the client, so keep the text
// short and never put anything secret in a flash; a payload over MaxCookieBytes is
// refused with ErrTooLarge rather than dropped silently by the browser.
//
// CacheStore keeps the messages in a cache.Store and sends the client only a signed
// random ticket. Use it for longer text, or when the message must not reach the
// client until it is rendered. Bring a durable Store when the app runs on more than
// one instance: the shipped LRU memory store may evict a message before its page
// loads, and another instance cannot see it at all.
//
// # Usage
//
//	flashes, err := flash.NewCookieStore(codec)
//
//	// in the handler that writes:
//	_ = flashes.Set(w, r, flash.Success("the invoice is sent"))
//	http.Redirect(w, r, "/invoices", http.StatusSeeOther)
//
//	// in the handler that renders the next page:
//	msgs, err := flashes.Take(w, r)
//
// Set replaces what the same response already staged, so pass every message in one
// call. Both stores treat a missing, expired, or unverifiable cookie as "no
// messages, no error": a lost flash is not worth failing a page render over. Only a
// failing cache.Store is reported, as ErrStore.
//
// A decoded message whose level is not one of the four constants is dropped, so
// nothing a client can influence reaches a template as a level.
//
// # htmx
//
// A response that stays on the page needs no cookie: render the message as an
// out-of-band fragment instead and let htmx swap it into the toaster. Redirects are
// where this package earns its keep, and web/htmx's Redirect helpers speak the htmx
// side of that.
package flash
