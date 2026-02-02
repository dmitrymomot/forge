package hostrouter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/pkg/hostrouter"
)

func TestRouter_ExactHost(t *testing.T) {
	routes := hostrouter.Routes{
		"example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("example"))
		}),
		"other.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("other"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
		wantCode int
	}{
		{"exact match", "example.com", "example", 200},
		{"exact match other", "other.com", "other", 200},
		{"case insensitive", "Example.COM", "example", 200},
		{"with port", "example.com:8080", "example", 200},
		{"no match", "unknown.com", "404 page not found\n", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("got body %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRouter_WildcardHost(t *testing.T) {
	routes := hostrouter.Routes{
		"*.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("wildcard"))
		}),
		"specific.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("specific"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
		wantCode int
	}{
		{"specific takes priority", "specific.example.com", "specific", 200},
		{"wildcard match foo", "foo.example.com", "wildcard", 200},
		{"wildcard match bar", "bar.example.com", "wildcard", 200},
		{"wildcard case insensitive", "FOO.Example.COM", "wildcard", 200},
		{"no match - root domain", "example.com", "404 page not found\n", 404},
		{"no match - other domain", "other.com", "404 page not found\n", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("got body %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRouter_FallbackToDefault(t *testing.T) {
	routes := hostrouter.Routes{
		"api.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("api"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("default"))
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
	}{
		{"api host", "api.example.com", "api"},
		{"other host uses fallback", "www.example.com", "default"},
		{"unknown host uses fallback", "unknown.com", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Body.String() != tt.wantBody {
				t.Errorf("got body %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRouter_EmptyRoutes(t *testing.T) {
	routes := hostrouter.Routes{}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fallback"))
	})

	router := hostrouter.New(routes, fallback)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Body.String() != "fallback" {
		t.Errorf("got body %q, want fallback", rec.Body.String())
	}
}

func TestRouter_IPv6Host(t *testing.T) {
	routes := hostrouter.Routes{
		"[::1]": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ipv6"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	// IPv6 addresses with port keep the brackets
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "[::1]"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Body.String() != "ipv6" {
		t.Errorf("got body %q, want ipv6", rec.Body.String())
	}
}
