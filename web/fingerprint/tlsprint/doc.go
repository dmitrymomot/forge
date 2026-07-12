// Package tlsprint contributes a TLS (JA4) fingerprint as the "tls" component of
// a web/fingerprint Fingerprinter. Two paths: trusted upstream headers
// (Cloudflare/CloudFront/Envoy/Caddy/Traefik) for CDN-terminated TLS, and a
// net.Listener wrapper computing JA4 from the raw ClientHello when the Go server
// terminates TLS. Header sources are trust-gated; an untrusted header is dropped.
//
// The local path assumes the ClientHello fits one TLS record (<=16KB, true for
// typical hellos). A ClientHello fragmented across records, or one whose
// declared record length exceeds 16 KiB, yields an empty JA4 (graceful
// degradation) without breaking the connection. The listener reads that
// record using the connection's existing deadline and sets none of its own;
// pair it with an http.Server.ReadTimeout (and/or ReadHeaderTimeout) to bound
// the read and guard against slow-loris clients, as with any TLS server.
package tlsprint
