package tlsprint

import (
	"crypto/tls"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

type requestTLSCollector struct{}

// RequestTLS returns a Collector that emits a coarse "tlsconn" component —
// "<version>|<cipher-hex>|<alpn>" (e.g. "1.3|c02b|h2") — from the request's own
// TLS handshake (http.Request.TLS). Use it when the Go server terminates
// crypto/tls directly, with no CDN JA header and no wrapped Listener. It is NOT
// a JA3/JA4 hash, so it uses a distinct component name and does not feed
// tls-ua-mismatch. A plaintext request (r.TLS == nil) contributes nothing.
//
// The parent fingerprint package cannot wire this into the Antifraud preset
// (that would import this subpackage back — a cycle), so compose it yourself:
//
//	tlsprint.Chain(tlsprint.CloudFrontJA4(trust), tlsprint.RequestTLS())
func RequestTLS() fingerprint.Collector { return requestTLSCollector{} }

func (requestTLSCollector) Collect(r *http.Request) ([]fingerprint.Component, error) {
	if r.TLS == nil {
		return nil, nil
	}
	v := tlsVersionString(r.TLS.Version) + "|" +
		strconv.FormatUint(uint64(r.TLS.CipherSuite), 16) + "|" +
		r.TLS.NegotiatedProtocol
	return []fingerprint.Component{{Name: "tlsconn", Value: v}}, nil
}

// tlsVersionString renders a TLS version constant compactly ("1.2", "1.3"),
// falling back to a hex code for unknown values.
func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "1.3"
	case tls.VersionTLS12:
		return "1.2"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS10:
		return "1.0"
	default:
		return "0x" + strconv.FormatUint(uint64(v), 16)
	}
}
