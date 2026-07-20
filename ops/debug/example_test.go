package debug_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/ops/debug"
)

func ExampleHandler() {
	// Mount the surface into an existing internal mux; paths are absolute, so
	// no StripPrefix. Gate it with debug.WithBasicAuth(users) in real wiring.
	mux := http.NewServeMux()
	mux.Handle("/debug/", debug.Handler())

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/stats")
	if err != nil || resp == nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	fmt.Println(resp.StatusCode, resp.Header.Get("Content-Type"))
	// Output: 200 application/json
}
