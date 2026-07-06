package secheaders_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/secheaders"
)

func ExampleNew() {
	mw, err := secheaders.New()
	if err != nil {
		panic(err)
	}

	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := middleware.Wrap(mux, mw)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	fmt.Println("X-Content-Type-Options:", rec.Header().Get("X-Content-Type-Options"))
	fmt.Println("X-Frame-Options:", rec.Header().Get("X-Frame-Options"))
	// Output:
	// X-Content-Type-Options: nosniff
	// X-Frame-Options: DENY
}
