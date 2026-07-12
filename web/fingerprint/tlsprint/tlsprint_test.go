package tlsprint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint"
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

func TestHeaderSourceNilTrustFuncFailsClosed(t *testing.T) {
	src := tlsprint.CloudflareJA3(nil)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cf-Ja3-Hash", "abc123")

	comps, err := src.Collect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comps) != 0 {
		t.Fatalf("nil TrustFunc must trust nothing and drop the header: %+v", comps)
	}
}

func TestTrustRangesPanicsOnInvalidCIDR(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TrustRanges must panic on invalid CIDR")
		}
	}()
	tlsprint.TrustRanges("not-a-cidr")
}

func TestTrustRangesInAndOutOfRange(t *testing.T) {
	trusted := tlsprint.TrustRanges("10.0.0.0/8")

	inRange := httptest.NewRequest("GET", "/", nil)
	inRange.RemoteAddr = "10.1.2.3:1234"
	if !trusted(inRange) {
		t.Fatal("peer inside the trusted range must be trusted")
	}

	outOfRange := httptest.NewRequest("GET", "/", nil)
	outOfRange.RemoteAddr = "8.8.8.8:1234"
	if trusted(outOfRange) {
		t.Fatal("peer outside the trusted range must not be trusted")
	}
}

func TestChainFirstNonEmptyWins(t *testing.T) {
	empty := fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
		return nil, nil
	})
	nonEmpty := fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
		return []fingerprint.Component{{Name: "tls", Value: "X"}}, nil
	})

	src := tlsprint.Chain(empty, nonEmpty)
	r := httptest.NewRequest("GET", "/", nil)
	comps, err := src.Collect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comps) != 1 || comps[0].Name != "tls" || comps[0].Value != "X" {
		t.Fatalf("Chain must return first non-empty result: %+v", comps)
	}
}

func TestChainAllEmptyYieldsNothing(t *testing.T) {
	empty := fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
		return nil, nil
	})

	src := tlsprint.Chain(empty, empty)
	r := httptest.NewRequest("GET", "/", nil)
	comps, err := src.Collect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comps) != 0 {
		t.Fatalf("all-empty Chain must yield no component: %+v", comps)
	}
}

func TestTrustPrivateProxies(t *testing.T) {
	src := tlsprint.CloudflareJA3(tlsprint.TrustPrivateProxies())

	loopback := httptest.NewRequest("GET", "/", nil)
	loopback.RemoteAddr = "127.0.0.1:1234"
	loopback.Header.Set("Cf-Ja3-Hash", "abc123")
	comps, _ := src.Collect(loopback)
	if len(comps) != 1 || comps[0].Value != "abc123" {
		t.Fatalf("loopback peer must be trusted: %+v", comps)
	}

	private := httptest.NewRequest("GET", "/", nil)
	private.RemoteAddr = "10.0.0.5:1234"
	private.Header.Set("Cf-Ja3-Hash", "abc123")
	comps, _ = src.Collect(private)
	if len(comps) != 1 || comps[0].Value != "abc123" {
		t.Fatalf("private-network peer must be trusted: %+v", comps)
	}

	public := httptest.NewRequest("GET", "/", nil)
	public.RemoteAddr = "8.8.8.8:1234"
	public.Header.Set("Cf-Ja3-Hash", "abc123")
	comps, _ = src.Collect(public)
	if len(comps) != 0 {
		t.Fatalf("public peer must not be trusted: %+v", comps)
	}
}

func TestCloudFrontJA4(t *testing.T) {
	src := tlsprint.CloudFrontJA4(func(_ *http.Request) bool { return true })
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("CloudFront-Viewer-JA4-Fingerprint", "ja4value")

	comps, err := src.Collect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comps) != 1 || comps[0].Name != "tls" || comps[0].Value != "ja4value" {
		t.Fatalf("trusted CloudFrontJA4 must emit tls component: %+v", comps)
	}
}

func TestHeaderCustomName(t *testing.T) {
	src := tlsprint.Header("X-Foo", func(_ *http.Request) bool { return true })
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Foo", "foovalue")

	comps, err := src.Collect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comps) != 1 || comps[0].Name != "tls" || comps[0].Value != "foovalue" {
		t.Fatalf("trusted Header source must emit tls component: %+v", comps)
	}
}
