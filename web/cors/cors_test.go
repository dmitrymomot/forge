package cors_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/cors"
	"github.com/dmitrymomot/forge/web/middleware"
)

func newMW(t *testing.T, cfg cors.Config, opts ...cors.Option) http.Handler {
	t.Helper()
	mw, err := cors.New(append([]cors.Option{cors.WithConfig(cfg)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // sentinel: handler reached
	}), mw)
}

func TestNonCORSPassesUntouched(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("non-CORS request must reach handler, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no ACAO without Origin header")
	}
}

func TestActualRequestAllowedOrigin(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}, ExposedHeaders: []string{"X-Total-Count"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "X-Total-Count" {
		t.Fatalf("ACEH = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Fatal("Vary: Origin missing")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatal("actual request must reach handler")
	}
}

func TestDisallowedOriginServedWithoutHeaders(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://evil.example.net")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("disallowed origin still served (browser enforces), got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("ACAO must be absent for disallowed origin")
	}
}

func TestPreflight(t *testing.T) {
	cfg := cors.DefaultConfig()
	cfg.AllowedOrigins = []string{"https://app.example.com"}
	cfg.AllowCredentials = true
	cfg.MaxAge = 600000000000 // 10m in ns
	h := newMW(t, cfg)
	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("Access-Control-Request-Method", "PUT")
	r.Header.Set("Access-Control-Request-Headers", "Content-Type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
		hd.Get("Access-Control-Allow-Credentials") != "true" ||
		!strings.Contains(hd.Get("Access-Control-Allow-Methods"), "PUT") ||
		hd.Get("Access-Control-Allow-Headers") != "Content-Type" ||
		hd.Get("Access-Control-Max-Age") != "600" {
		t.Fatalf("preflight headers wrong: %+v", hd)
	}
	vary := strings.Join(hd.Values("Vary"), ", ")
	for _, want := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !strings.Contains(vary, want) {
			t.Fatalf("Vary missing %s: %q", want, vary)
		}
	}
}

func TestPreflightDisallowedOriginNoHeaders(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://evil.example.net")
	r.Header.Set("Access-Control-Request-Method", "PUT")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight always terminates, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no CORS headers for disallowed preflight")
	}
}

func TestWildcardSubdomain(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://*.example.com"}})
	tests := []struct {
		origin string
		want   bool
	}{
		{"https://a.example.com", true},
		{"https://a.b.example.com", false}, // single label only
		{"https://example.com", false},     // base itself not covered
		{"http://a.example.com", false},    // scheme must match
		{"https://aexample.com", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Origin", tt.origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		got := rec.Header().Get("Access-Control-Allow-Origin") != ""
		if got != tt.want {
			t.Errorf("origin %s allowed=%v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestBareWildcard(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"*"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://anywhere.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO = %q, want *", got)
	}
}

func TestWildcardWithCredentialsRejected(t *testing.T) {
	_, err := cors.New(cors.WithConfig(cors.Config{AllowedOrigins: []string{"*"}, AllowCredentials: true}))
	if !errors.Is(err, cors.ErrInvalidConfig) {
		t.Fatalf("bare * + credentials must be rejected, got %v", err)
	}
}

func TestOriginFuncOverrides(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://never.example"}},
		cors.WithOriginFunc(func(origin string) bool { return strings.HasSuffix(origin, ".tenant.example") }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://acme.tenant.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://acme.tenant.example" {
		t.Fatal("origin func must decide allowance")
	}
}

func TestCredentialsEchoOriginNeverStar(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}, AllowCredentials: true})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("with credentials ACAO must echo origin, got %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("ACAC missing")
	}
}

func TestInvalidPatternRejected(t *testing.T) {
	for _, bad := range []string{"https://a.*.example.com", "*.example.com", "https://*", "https://*.", "ftp//x"} {
		if _, err := cors.New(cors.WithConfig(cors.Config{AllowedOrigins: []string{bad}})); !errors.Is(err, cors.ErrInvalidConfig) {
			t.Errorf("pattern %q must be rejected, got %v", bad, err)
		}
	}
}
