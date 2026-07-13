package guard_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func TestBearerHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   string
		wantOK bool
	}{
		{"valid", "Bearer tok123", "tok123", true},
		{"lowercase scheme", "bearer tok123", "tok123", true},
		{"extra spaces after scheme", "Bearer   tok123", "tok123", true},
		{"absent", "", "", false},
		{"scheme only", "Bearer", "", false},
		{"scheme with empty token", "Bearer   ", "", false},
		{"basic scheme reads as no credential", "Basic dXNlcjpwYXNz", "", false},
		{"no scheme", "tok123", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			got, ok := guard.BearerHeader()(r)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("BearerHeader() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-API-Key", "key123")

	if got, ok := guard.Header("X-API-Key")(r); !ok || got != "key123" {
		t.Fatalf("Header(X-API-Key) = (%q, %v), want (key123, true)", got, ok)
	}
	if got, ok := guard.Header("X-Missing")(r); ok {
		t.Fatalf("Header(X-Missing) = (%q, %v), want ok=false", got, ok)
	}
	r.Header.Set("X-Empty", "")
	if _, ok := guard.Header("X-Empty")(r); ok {
		t.Fatal("Header(X-Empty): empty value must read as no credential")
	}
	// name canonicalization must stay case-insensitive regardless of the
	// casing passed in (guard.Header pre-canonicalizes once at construction).
	if got, ok := guard.Header("x-api-key")(r); !ok || got != "key123" {
		t.Fatalf("Header(x-api-key) = (%q, %v), want (key123, true)", got, ok)
	}
}

func TestCookie(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
	r.AddCookie(&http.Cookie{Name: "empty", Value: ""})

	if got, ok := guard.Cookie("sid")(r); !ok || got != "abc" {
		t.Fatalf("Cookie(sid) = (%q, %v), want (abc, true)", got, ok)
	}
	if _, ok := guard.Cookie("missing")(r); ok {
		t.Fatal("Cookie(missing): want ok=false")
	}
	if _, ok := guard.Cookie("empty")(r); ok {
		t.Fatal("Cookie(empty): empty value must read as no credential")
	}
}

func TestQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/path?token=qtok&empty=", nil)

	if got, ok := guard.Query("token")(r); !ok || got != "qtok" {
		t.Fatalf("Query(token) = (%q, %v), want (qtok, true)", got, ok)
	}
	if _, ok := guard.Query("missing")(r); ok {
		t.Fatal("Query(missing): want ok=false")
	}
	if _, ok := guard.Query("empty")(r); ok {
		t.Fatal("Query(empty): empty value must read as no credential")
	}

	enc := httptest.NewRequest(http.MethodGet, "/p?tok%65n=q%74ok&plus=a+b&first=1&first=2", nil)
	if got, ok := guard.Query("token")(enc); !ok || got != "qtok" {
		t.Fatalf("Query(token) encoded = (%q, %v), want (qtok, true)", got, ok)
	}
	if got, ok := guard.Query("plus")(enc); !ok || got != "a b" {
		t.Fatalf("Query(plus) = (%q, %v), want (a b, true)", got, ok)
	}
	if got, ok := guard.Query("first")(enc); !ok || got != "1" {
		t.Fatalf("Query(first) = (%q, %v), want first occurrence (1, true)", got, ok)
	}
}
