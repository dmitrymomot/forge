package hostrouter_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/hostrouter"
)

func Example() {
	tenant := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "tenant=%s", hostrouter.Subdomain(r.Context()))
	})
	router := hostrouter.New(
		hostrouter.WithHost("*.example.com", tenant),
	)

	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "acme.example.com"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	// Output: tenant=acme
}
