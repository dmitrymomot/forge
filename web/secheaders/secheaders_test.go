package secheaders_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/secheaders"
)

func serve(t *testing.T, mw middleware.Middleware, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	middleware.Wrap(h, mw).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec
}

func TestDefaultHeaders(t *testing.T) {
	mw, err := secheaders.New()
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {})
	want := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "strict-origin-when-cross-origin",
		"X-Frame-Options":            "DENY",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must be off by default")
	}
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("CSP must be off by default")
	}
}

func TestHSTSFromConfig(t *testing.T) {
	cfg := secheaders.DefaultConfig()
	cfg.HSTSMaxAge = 180 * 24 * time.Hour
	cfg.HSTSIncludeSubdomains = true
	mw, err := secheaders.New(secheaders.WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {})
	got := rec.Header().Get("Strict-Transport-Security")
	if !strings.Contains(got, "max-age=15552000") || !strings.Contains(got, "includeSubDomains") {
		t.Fatalf("HSTS = %q", got)
	}
}

func TestFrameOptionsOff(t *testing.T) {
	cfg := secheaders.DefaultConfig()
	cfg.FrameOptions = "off"
	mw, err := secheaders.New(secheaders.WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {})
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Fatal("FrameOptions=off must suppress the header")
	}
}

func TestInvalidFrameOptions(t *testing.T) {
	cfg := secheaders.DefaultConfig()
	cfg.FrameOptions = "BOGUS"
	if _, err := secheaders.New(secheaders.WithConfig(cfg)); !errors.Is(err, secheaders.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCSPWithNonce(t *testing.T) {
	mw, err := secheaders.New(
		secheaders.WithCSP(secheaders.Policy{
			DefaultSrc: []string{secheaders.Self},
			ScriptSrc:  []string{secheaders.Self},
		}),
		secheaders.WithNonce(),
	)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	h := func(w http.ResponseWriter, r *http.Request) {
		n := secheaders.Nonce(r.Context())
		seen = append(seen, n)
		_, _ = io.WriteString(w, n)
	}
	rec1 := serve(t, mw, h)
	rec2 := serve(t, mw, h)
	if len(seen) != 2 {
		t.Fatalf("handler must run twice, captured %d nonces", len(seen))
	}
	if seen[0] == "" || seen[0] == seen[1] {
		t.Fatalf("nonces must be unique per request: %q vs %q", seen[0], seen[1])
	}
	csp := rec1.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("CSP missing default-src: %q", csp)
	}
	if !strings.Contains(csp, "'nonce-"+seen[0]+"'") {
		t.Fatalf("CSP missing request nonce: %q", csp)
	}
	csp2 := rec2.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp2, "'nonce-"+seen[1]+"'") {
		t.Fatalf("second response CSP missing its nonce: %q", csp2)
	}
}

func TestHandlerOverrideWins(t *testing.T) {
	mw, err := secheaders.New()
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	})
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("handler override lost: %q", got)
	}
}

func TestNonceOutsideMiddlewareIsEmpty(t *testing.T) {
	if secheaders.Nonce(httptest.NewRequest(http.MethodGet, "/", nil).Context()) != "" {
		t.Fatal("Nonce outside middleware must be empty")
	}
}
