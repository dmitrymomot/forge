package hostrouter_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/hostrouter"
)

func Example() {
	tenant := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "tenant=%s", hostrouter.Subdomain(r.Context()))
	})
	router, err := hostrouter.New(
		hostrouter.WithHost("*.example.com", tenant),
	)
	if err != nil {
		panic(err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "acme.example.com"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	// Output: tenant=acme
}

// ExampleWithLookup resolves a customer's own domain at request time. The lookup runs
// after the static patterns, so a customer domain can never shadow a platform host,
// and it fails closed: a store error answers 503 instead of falling through to 404.
func ExampleWithLookup() {
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "served %s", hostrouter.Host(r.Context()))
	})

	customDomains := map[string]bool{"shop.customer.tld": true}
	router, err := hostrouter.New(
		hostrouter.WithHost("*.example.com", app),
		hostrouter.WithLookup(func(_ context.Context, host string) (http.Handler, error) {
			if !customDomains[host] {
				return nil, hostrouter.ErrHostNotFound
			}
			return app, nil
		}),
	)
	if err != nil {
		panic(err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "shop.customer.tld"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	// Output: served shop.customer.tld
}
