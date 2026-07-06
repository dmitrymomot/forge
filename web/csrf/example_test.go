package csrf_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/csrf"
	"github.com/dmitrymomot/forge/web/middleware"
)

func ExampleNew() {
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		panic(err)
	}
	codec, err := cookie.New(ks)
	if err != nil {
		panic(err)
	}

	var mintedToken string
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintedToken = csrf.Token(r)
	})
	h := middleware.Wrap(mux, csrf.New(codec))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	fmt.Println(mintedToken != "")
	// Output:
	// true
}
