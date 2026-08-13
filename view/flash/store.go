package flash

import (
	"net/http"
)

// Store holds messages between the response that stages them and the next request
// that reads them. Set stages, Take returns everything staged by the previous
// response and clears it, so a reload shows nothing.
//
// Both methods take the ResponseWriter because every implementation writes a cookie:
// the payload itself for CookieStore, a claim ticket for CacheStore. Set replaces
// rather than appends, so a response says everything it has to say in one call.
type Store interface {
	Set(w http.ResponseWriter, r *http.Request, msgs ...Message) error
	Take(w http.ResponseWriter, r *http.Request) ([]Message, error)
}

// Setter returns a func that stages msgs on a response, which is the shape a
// response-writing layer accepts without importing this package's types:
//
//	return respond.SeeOther("/invoices",
//		respond.WithBefore(flash.Setter(flashes, r, flash.Success("the invoice is sent")))), nil
func Setter(store Store, r *http.Request, msgs ...Message) func(http.ResponseWriter) error {
	return func(w http.ResponseWriter) error {
		return store.Set(w, r, msgs...)
	}
}

// withText drops messages carrying no text, so an empty flash never round-trips as a
// blank toast.
func withText(msgs []Message) []Message {
	kept := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Text != "" {
			kept = append(kept, m)
		}
	}
	return kept
}
