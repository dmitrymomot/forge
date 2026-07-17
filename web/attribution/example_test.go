package attribution_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/attribution"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/middleware"
)

func Example() {
	ks, _ := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	codec, _ := cookie.New(ks)
	tracker := attribution.New(codec)

	// The campaign landing: middleware records the touch into a signed cookie.
	landing := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), tracker.Middleware())
	rec := httptest.NewRecorder()
	landing.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?utm_source=google&utm_campaign=launch", nil))

	// The conversion, days later: the browser sends the cookie back.
	signup := httptest.NewRequest(http.MethodPost, "/signup", nil)
	for _, ck := range rec.Result().Cookies() {
		signup.AddCookie(ck)
	}
	touch, err := tracker.Touch(signup)
	if err != nil {
		fmt.Println("organic signup")
		return
	}
	fmt.Println(touch.Get("utm_source"), touch.Get("utm_campaign"))
	// Output: google launch
}
