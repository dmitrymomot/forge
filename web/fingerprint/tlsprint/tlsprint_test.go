package tlsprint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint/tlsprint"
)

func TestHeaderSourceTrustGate(t *testing.T) {
	// Untrusted -> component dropped.
	src := tlsprint.CloudflareJA3(func(_ *http.Request) bool { return false })
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cf-Ja3-Hash", "abc123")
	comps, _ := src.Collect(r)
	if len(comps) != 0 {
		t.Fatalf("untrusted header must be dropped: %+v", comps)
	}

	// Trusted -> emits tls component.
	src = tlsprint.CloudflareJA3(func(_ *http.Request) bool { return true })
	comps, _ = src.Collect(r)
	if len(comps) != 1 || comps[0].Name != "tls" || comps[0].Value != "abc123" {
		t.Fatalf("trusted header not emitted: %+v", comps)
	}
}
