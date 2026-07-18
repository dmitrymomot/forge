package pagination_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/pagination"
)

func BenchmarkParseDefault(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := pagination.Parse(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseValues(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/?page=47&per_page=25", nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := pagination.Parse(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseCursorDefault(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := pagination.ParseCursor(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseCursorValues(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/?cursor=next-page&limit=25", nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := pagination.ParseCursor(r); err != nil {
			b.Fatal(err)
		}
	}
}
