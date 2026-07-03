package htmx_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/htmx"
)

func ExampleIsRequest() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cart", nil)
	r.Header.Set("HX-Request", "true")

	if htmx.IsRequest(r) {
		htmx.Retarget(rec, "#cart")
		htmx.Trigger(rec, "cart:updated")
		// then render the partial, e.g. render.Templ(r.Context(), rec, 200, fragment)
	}

	fmt.Println(htmx.IsRequest(r))
	fmt.Println(rec.Header().Get("HX-Retarget"))
	fmt.Println(rec.Header().Get("HX-Trigger"))
	// Output:
	// true
	// #cart
	// cart:updated
}

func ExampleRedirect() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login", nil) // non-HTMX request

	htmx.Redirect(rec, r, "/dashboard") // fallback status defaults to 303 See Other

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Location"))
	// Output:
	// 303
	// /dashboard
}

func ExampleRedirectBack() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login?redirect=/dashboard", nil) // non-HTMX request

	htmx.RedirectBack(rec, r, "/home") // honors the safe local ?redirect=, else /home

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Location"))
	// Output:
	// 303
	// /dashboard
}

func ExampleRedirectExternal() {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/checkout", nil)
	r.Header.Set("HX-Request", "true") // HTMX request

	htmx.RedirectExternal(rec, r, "https://pay.example.com/session/42")

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("HX-Redirect"))
	// Output:
	// 200
	// https://pay.example.com/session/42
}
