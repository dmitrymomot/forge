package cookie_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/cookie"
)

func ExampleCodec_SetSigned() {
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		panic(err)
	}
	codec, err := cookie.New(ks)
	if err != nil {
		panic(err)
	}

	rec := httptest.NewRecorder()
	_ = codec.SetSigned(rec, "__Host-csrf", "token-value")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range rec.Result().Cookies() {
		r.AddCookie(ck)
	}

	value, err := codec.GetSigned(r, "__Host-csrf")
	fmt.Println(value, err)
	// Output:
	// token-value <nil>
}
