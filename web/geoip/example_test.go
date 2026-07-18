package geoip_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/geoip"
)

// Example wires a header-only source (no database file needed) and reads the
// cached Location in a handler.
func Example() {
	src := geoip.Cloudflare()
	h := geoip.Middleware(src)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fmt.Println(geoip.Get(r).CountryCode)
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("CF-IPCountry", "US")
	h.ServeHTTP(httptest.NewRecorder(), r)
	// Output: US
}
