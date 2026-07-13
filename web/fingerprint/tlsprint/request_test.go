package tlsprint_test

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint/tlsprint"
)

func TestRequestTLSCollect(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{
		Version:            tls.VersionTLS13,
		CipherSuite:        tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, // 0xc02b
		NegotiatedProtocol: "h2",
	}
	comps, err := tlsprint.RequestTLS().Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || comps[0].Name != "tlsconn" || comps[0].Value != "1.3|c02b|h2" {
		t.Fatalf("unexpected component: %+v", comps)
	}
}

func TestRequestTLSPlaintextEmitsNothing(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil) // r.TLS == nil
	comps, err := tlsprint.RequestTLS().Collect(r)
	if err != nil || comps != nil {
		t.Fatalf("plaintext request must emit nothing: %+v, %v", comps, err)
	}
}
