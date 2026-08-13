package flash_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/view/flash"
	"github.com/dmitrymomot/forge/web/cookie"
)

// Example shows post/redirect/get: the handler that writes stages the message and
// redirects, and the page the browser lands on reads it once.
func Example() {
	ks, _ := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	codec, _ := cookie.New(ks)
	flashes, _ := flash.NewCookieStore(codec)

	pay := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = flashes.Set(w, r, flash.Success("the invoice is paid"))
		http.Redirect(w, r, "/invoices", http.StatusSeeOther)
	})

	invoices := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msgs, _ := flashes.Take(w, r)
		for _, m := range msgs {
			fmt.Printf("%s: %s\n", m.Level, m.Text)
		}
	})

	payRec := httptest.NewRecorder()
	pay.ServeHTTP(payRec, httptest.NewRequest(http.MethodPost, "/invoices/1/pay", nil))

	next := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	for _, c := range payRec.Result().Cookies() {
		next.AddCookie(c)
	}
	invoices.ServeHTTP(httptest.NewRecorder(), next)

	// Output: success: the invoice is paid
}
