package fingerprint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestMiddlewareStashesResult(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	var got fingerprint.Result
	var ok bool
	h := fp.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = fingerprint.FromContext(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !ok || got.Fingerprint.Hash == "" {
		t.Fatalf("result not stashed: ok=%v %+v", ok, got)
	}
}
