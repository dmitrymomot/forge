package csrf_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/csrf"
	"github.com/dmitrymomot/forge/web/middleware"
)

func newCodec(t *testing.T, opts ...cookie.Option) *cookie.Codec {
	t.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cookie.New(ks, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// getToken performs a GET and returns the minted token and its cookies.
func getToken(t *testing.T, h http.Handler) (string, []*http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	token := strings.TrimSpace(rec.Body.String())
	if token == "" {
		t.Fatal("no token exposed on minting request")
	}
	return token, rec.Result().Cookies()
}

func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, csrf.Token(r))
	})
}

func TestGetMintsCookieAndExposesToken(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	if len(cks) != 1 || cks[0].Name != "__Host-csrf" {
		t.Fatalf("want minted __Host-csrf cookie, got %+v", cks)
	}
	if token == "" {
		t.Fatal("Token(r) empty on minting request")
	}
}

func TestHostPrefixFallback(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t, cookie.WithSecure(false))))
	_, cks := getToken(t, h)
	if len(cks) != 1 || cks[0].Name != "csrf" {
		t.Fatalf("insecure codec must fall back to plain csrf name, got %+v", cks)
	}
}

func TestPostWithHeaderTokenPasses(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	for _, ck := range cks {
		r.AddCookie(ck)
	}
	r.Header.Set("X-CSRF-Token", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST with valid header token = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestPostWithFormTokenPasses(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	form := url.Values{"csrf_token": {token}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, ck := range cks {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST with valid form token = %d", rec.Code)
	}
}

func TestPostRejections(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)

	tests := []struct {
		name  string
		setup func(r *http.Request)
	}{
		{"no cookie no token", func(r *http.Request) {}},
		{"cookie but no echo", func(r *http.Request) {
			for _, ck := range cks {
				r.AddCookie(ck)
			}
		}},
		{"wrong token", func(r *http.Request) {
			for _, ck := range cks {
				r.AddCookie(ck)
			}
			r.Header.Set("X-CSRF-Token", "wrong-"+token)
		}},
		{"tampered cookie", func(r *http.Request) {
			bad := *cks[0]
			bad.Value = "x" + bad.Value[1:]
			r.AddCookie(&bad)
			r.Header.Set("X-CSRF-Token", token)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			tt.setup(r)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("want 403, got %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Fatalf("want problem+json rejection, got %q", ct)
			}
		})
	}
}

func TestSafeMethodsPass(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, want 200", m, rec.Code)
		}
	}
}

func TestSkipPredicateBypasses(t *testing.T) {
	mw := csrf.New(newCodec(t), csrf.WithSkip(func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/webhooks/")
	}))
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), mw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("skipped route = %d, want 200", rec.Code)
	}
}

func TestTokenStableAcrossRequests(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range cks {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := strings.TrimSpace(rec.Body.String()); got != token {
		t.Fatalf("token must be stable across requests: %q != %q", got, token)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("valid cookie must not be re-minted")
	}
}
