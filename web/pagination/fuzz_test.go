package pagination_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/dmitrymomot/forge/web/pagination"
)

func FuzzParse(f *testing.F) {
	for _, seed := range [][2]string{
		{"", ""},
		{"1", "20"},
		{"0", "0"},
		{"2147483647", "100"},
		{"wrong", "20"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, page, limit string) {
		q := url.Values{}
		q.Set("page", page)
		q.Set("per_page", limit)
		r := httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil)

		p, err := pagination.Parse(r)
		if err != nil {
			return
		}
		if p.Limit < 1 || p.Limit > 100 {
			t.Fatalf("limit = %d, want 1..100", p.Limit)
		}
		if p.Offset < 0 {
			t.Fatalf("offset = %d, want non-negative", p.Offset)
		}
		if p.Offset%p.Limit != 0 {
			t.Fatalf("offset %d is not divisible by limit %d", p.Offset, p.Limit)
		}
	})
}

func FuzzParseCursor(f *testing.F) {
	for _, seed := range [][2]string{
		{"", ""},
		{"next-page", "20"},
		{"next/page", "0"},
		{"wrong", "2147483647"},
		{"next", "wrong"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, cursor, limit string) {
		q := url.Values{}
		q.Set("cursor", cursor)
		q.Set("limit", limit)
		r := httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil)

		p, err := pagination.ParseCursor(r)
		if err != nil {
			return
		}
		if p.Limit < 1 || p.Limit > 100 {
			t.Fatalf("limit = %d, want 1..100", p.Limit)
		}
	})
}
