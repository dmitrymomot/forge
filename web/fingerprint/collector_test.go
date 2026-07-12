package fingerprint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestFromRequestHashesComponents(t *testing.T) {
	cfg := fingerprint.Config{Secret: "top-secret", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(
		fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
			return []fingerprint.Component{{Name: "ua", Value: "curl/8"}}, nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	f, err := fp.FromRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if f.Hash == "" || f.Version != 1 {
		t.Fatalf("bad fingerprint: %+v", f)
	}
	// Stable: same input -> same hash and non-empty digest parts.
	f2, _ := fp.FromRequest(r)
	if f.Hash != f2.Hash {
		t.Fatalf("unstable hash: %s vs %s", f.Hash, f2.Hash)
	}
	if _, ok := f.Digest().Parts["ua"]; !ok {
		t.Fatalf("digest missing ua part: %+v", f.Digest())
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	if _, err := fingerprint.New(fingerprint.Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
}
