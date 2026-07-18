package pagination_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/pagination"
)

func ExampleParse() {
	r := httptest.NewRequest(http.MethodGet, "/users?page=3&per_page=25", nil)
	p, err := pagination.Parse(r)
	if err != nil {
		panic(err)
	}

	// Copy these values into the generated params type for the static sqlc query.
	fmt.Printf("LIMIT %d OFFSET %d\n", p.Limit, p.Offset)
	// Output:
	// LIMIT 25 OFFSET 50
}

func ExampleParseCursor() {
	r := httptest.NewRequest(http.MethodGet, "/users?cursor=next-page&limit=25", nil)
	p, err := pagination.ParseCursor(r)
	if err != nil {
		panic(err)
	}

	// Copy these values into the generated params type for the static sqlc query.
	fmt.Printf("CURSOR %q LIMIT %d\n", p.Cursor, p.Limit)
	// Output:
	// CURSOR "next-page" LIMIT 25
}
