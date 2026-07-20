package debug_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/ops/debug"
	"github.com/dmitrymomot/forge/web/middleware"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandlerRoutes(t *testing.T) {
	t.Parallel()
	h := debug.Handler()

	t.Run("stats", func(t *testing.T) {
		t.Parallel()
		rec := get(t, h, "/debug/stats")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		var s debug.Stats
		if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if s.Goroutines <= 0 || s.GoVersion == "" || s.NumCPU <= 0 {
			t.Fatalf("implausible stats: %+v", s)
		}
	})

	t.Run("vars", func(t *testing.T) {
		t.Parallel()
		rec := get(t, h, "/debug/vars")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var vars map[string]json.RawMessage
		if err := json.NewDecoder(rec.Body).Decode(&vars); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := vars["memstats"]; !ok {
			t.Fatalf("expvar output missing memstats key: %v", vars)
		}
	})

	t.Run("pprof index", func(t *testing.T) {
		t.Parallel()
		rec := get(t, h, "/debug/pprof/")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		if !strings.Contains(string(body), "goroutine") {
			t.Fatalf("pprof index does not list goroutine profile")
		}
	})

	t.Run("pprof named profile", func(t *testing.T) {
		t.Parallel()
		rec := get(t, h, "/debug/pprof/goroutine?debug=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("pprof cmdline", func(t *testing.T) {
		t.Parallel()
		if rec := get(t, h, "/debug/pprof/cmdline"); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("root index", func(t *testing.T) {
		t.Parallel()
		rec := get(t, h, "/")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		for _, path := range []string{"/debug/pprof/", "/debug/stats", "/debug/vars"} {
			if !strings.Contains(string(body), path) {
				t.Fatalf("index missing %s: %s", path, body)
			}
		}
	})

	t.Run("unknown path", func(t *testing.T) {
		t.Parallel()
		if rec := get(t, h, "/nope"); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestHandlerBasicAuth(t *testing.T) {
	t.Parallel()
	h := debug.Handler(debug.WithBasicAuth(map[string]string{"ops": "s3cret"}))

	t.Run("no credentials", func(t *testing.T) {
		t.Parallel()
		rec := get(t, h, "/debug/stats")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("missing WWW-Authenticate challenge")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/debug/stats", nil)
		req.SetBasicAuth("ops", "wrong")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("valid credentials", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/debug/stats", nil)
		req.SetBasicAuth("ops", "s3cret")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}

func TestWithBasicAuthEmptyUsersPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty users map")
		}
	}()
	debug.Handler(debug.WithBasicAuth(nil))
}

func TestHandlerCustomMiddleware(t *testing.T) {
	t.Parallel()
	var mw middleware.Middleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Token") != "ok" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	h := debug.Handler(debug.WithMiddleware(mw))

	if rec := get(t, h, "/debug/stats"); rec.Code != http.StatusForbidden {
		t.Fatalf("ungated status = %d, want 403", rec.Code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/stats", nil)
	req.Header.Set("X-Token", "ok")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gated status = %d, want 200", rec.Code)
	}
}
