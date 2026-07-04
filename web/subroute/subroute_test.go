package subroute_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/subroute"
)

// echo returns a handler that writes the request's method, path, raw path,
// and query so tests can assert exactly what the mounted handler observed.
func echo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s %s raw=%q q=%s", r.Method, r.URL.Path, r.URL.RawPath, r.URL.RawQuery)
	})
}

func get(t *testing.T, mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestMount_PanicsOnInvalidInput(t *testing.T) {
	h := echo()
	tests := []struct {
		name string
		fn   func()
	}{
		{"nil mux", func() { subroute.Mount(nil, "/a", h) }},
		{"nil handler", func() { subroute.Mount(http.NewServeMux(), "/a", nil) }},
		{"empty prefix", func() { subroute.Mount(http.NewServeMux(), "", h) }},
		{"no leading slash", func() { subroute.Mount(http.NewServeMux(), "admin", h) }},
		{"bare slash", func() { subroute.Mount(http.NewServeMux(), "/", h) }},
		{"trailing slash", func() { subroute.Mount(http.NewServeMux(), "/admin/", h) }},
		{"space smuggling method", func() { subroute.Mount(http.NewServeMux(), "GET /admin", h) }},
		{"empty segment", func() { subroute.Mount(http.NewServeMux(), "/a//b", h) }},
		{"dollar wildcard", func() { subroute.Mount(http.NewServeMux(), "/a/{$}", h) }},
		{"multi-segment wildcard", func() { subroute.Mount(http.NewServeMux(), "/a/{rest...}", h) }},
		{"empty wildcard name", func() { subroute.Mount(http.NewServeMux(), "/a/{}", h) }},
		{"partial wildcard", func() { subroute.Mount(http.NewServeMux(), "/a/x{id}", h) }},
		{"duplicate wildcard names", func() { subroute.Mount(http.NewServeMux(), "/{id}/x/{id}", h) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, tt.fn)
		})
	}
}

func TestMount_DuplicateRegistrationPanics(t *testing.T) {
	mux := http.NewServeMux()
	subroute.Mount(mux, "/admin", echo())
	assert.Panics(t, func() { subroute.Mount(mux, "/admin", echo()) })
}

func TestMount_StaticStripping(t *testing.T) {
	adminMux := http.NewServeMux()
	adminMux.Handle("GET /{$}", echo())
	adminMux.Handle("GET /users/{id}", echo())
	adminMux.Handle("POST /submit", echo())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "ok")
	})
	subroute.Mount(mux, "/admin", adminMux)
	subroute.Mount(mux, "/api/v1", echo())

	tests := []struct {
		name, method, target, wantBody string
		wantStatus                     int
	}{
		{"bare prefix serves as root", http.MethodGet, "/admin", `GET / raw="" q=`, http.StatusOK},
		{"trailing slash serves as root", http.MethodGet, "/admin/", `GET / raw="" q=`, http.StatusOK},
		{"subtree stripped", http.MethodGet, "/admin/users/42", `GET /users/42 raw="" q=`, http.StatusOK},
		{"method pattern inside mount", http.MethodPost, "/admin/submit", `POST /submit raw="" q=`, http.StatusOK},
		{"multi-segment static prefix", http.MethodGet, "/api/v1/orders/7", `GET /orders/7 raw="" q=`, http.StatusOK},
		{"query preserved", http.MethodGet, "/admin/users/42?page=2", `GET /users/42 raw="" q=page=2`, http.StatusOK},
		{"sibling route untouched", http.MethodGet, "/health", "ok", http.StatusOK},
		{"unknown path 404s on outer mux", http.MethodGet, "/nope", "", http.StatusNotFound},
		{"unknown path inside mount 404s", http.MethodGet, "/admin/nope", "", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))
			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.Equal(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}

func TestMount_EncodedSegments(t *testing.T) {
	mux := http.NewServeMux()
	subroute.Mount(mux, "/files", echo())

	// %2F must survive stripping: the mounted handler gets a consistent
	// Path/RawPath pair, and the escaped form keeps the encoded slash.
	rec := get(t, mux, "/files/a%2Fb/c")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `GET /a/b/c raw="/a%2Fb/c" q=`, rec.Body.String())
}

func TestMount_CallerRequestNotMutated(t *testing.T) {
	mux := http.NewServeMux()
	subroute.Mount(mux, "/admin", echo())

	req := httptest.NewRequest(http.MethodGet, "/admin/users/42", nil)
	mux.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "/admin/users/42", req.URL.Path)
}
