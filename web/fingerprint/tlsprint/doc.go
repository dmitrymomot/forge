// Package tlsprint contributes a TLS (JA4) fingerprint as the "tls" component of
// a web/fingerprint Fingerprinter. Two paths: trusted upstream headers
// (Cloudflare/CloudFront/Envoy/Caddy/Traefik) for CDN-terminated TLS, and a
// net.Listener wrapper computing JA4 from the raw ClientHello when the Go server
// terminates TLS. Header sources are trust-gated; an untrusted header is dropped.
//
// The local path assumes the ClientHello fits one TLS record (<=16KB, true for
// typical hellos). A ClientHello fragmented across records yields an empty JA4
// (graceful degradation) without breaking the connection.
package tlsprint
