package request_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/request"
)

func ExampleQuery() {
	r := httptest.NewRequest(http.MethodGet, "/search?page=2", nil)
	page, _ := request.Query[int](r, "page", 1)
	fmt.Println(page)
	// Output: 2
}

func ExampleDecodeJSON() {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ada"}`))
	r.Header.Set("Content-Type", "application/json")

	var v struct {
		Name string `json:"name"`
	}
	if err := request.DecodeJSON(r, &v); err != nil {
		fmt.Println("status:", request.StatusCode(err))
		return
	}
	fmt.Println(v.Name)
	// Output: ada
}

func BenchmarkQueryInt(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/?n=12345", nil)
	b.ReportAllocs()
	for range b.N {
		_, _ = request.Query[int](r, "n")
	}
}
