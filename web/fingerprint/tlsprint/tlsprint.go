// Package tlsprint contributes a TLS (JA3/JA4) fingerprint as the "tls"
// fingerprint component, sourced from headers set by a trusted upstream
// (Cloudflare, CloudFront, or a self-hosted proxy such as Envoy/Caddy/Traefik).
//
// A raw JA3/JA4 hash is only trustworthy when it was computed by something
// that actually terminated the TLS handshake and forwarded the result over a
// channel the app controls — never trust it straight from an arbitrary
// client-supplied header. TrustFunc expresses that gate: build one with
// TrustPrivateProxies or TrustRanges, then pass it to a header source. A nil
// TrustFunc trusts nothing (the header is always dropped) — the gate fails
// closed, never open.
package tlsprint

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

// TrustFunc reports whether a request provably transited a trusted proxy, so
// an upstream-set JA hash header may be believed.
type TrustFunc func(*http.Request) bool

// TrustPrivateProxies trusts requests whose immediate peer (RemoteAddr) is in
// a private/loopback/CGNAT range — the standard "app behind a reverse proxy
// on a private network" setup.
func TrustPrivateProxies() TrustFunc {
	ranges := clientip.PrivateRanges()
	return func(r *http.Request) bool { return peerInRanges(r, ranges) }
}

// TrustRanges trusts requests whose immediate peer is in one of the CIDRs. It
// panics on an invalid CIDR (call at startup).
func TrustRanges(cidrs ...string) TrustFunc {
	ranges := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			panic("tlsprint: TrustRanges: invalid CIDR " + c + ": " + err.Error())
		}
		ranges = append(ranges, p)
	}
	return func(r *http.Request) bool { return peerInRanges(r, ranges) }
}

func peerInRanges(r *http.Request, ranges []netip.Prefix) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, p := range ranges {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

type headerSource struct {
	trusted TrustFunc
	header  string
}

// CloudflareJA3 reads the Cf-Ja3-Hash header when trusted. A nil TrustFunc
// trusts nothing (the header is always dropped) — pass an explicit TrustFunc
// (e.g. TrustPrivateProxies) to enable the source.
func CloudflareJA3(trusted TrustFunc) fingerprint.Collector {
	return headerSource{header: "Cf-Ja3-Hash", trusted: trusted}
}

// CloudFrontJA4 reads the CloudFront-Viewer-JA4-Fingerprint header when
// trusted. A nil TrustFunc trusts nothing (the header is always dropped) —
// pass an explicit TrustFunc (e.g. TrustPrivateProxies) to enable the source.
func CloudFrontJA4(trusted TrustFunc) fingerprint.Collector {
	return headerSource{header: "CloudFront-Viewer-JA4-Fingerprint", trusted: trusted}
}

// Header reads an arbitrary upstream TLS-fingerprint header (Envoy/Caddy/
// Traefik) when trusted. A nil TrustFunc trusts nothing (the header is
// always dropped) — pass an explicit TrustFunc (e.g. TrustPrivateProxies) to
// enable the source.
func Header(name string, trusted TrustFunc) fingerprint.Collector {
	return headerSource{header: name, trusted: trusted}
}

func (s headerSource) Collect(r *http.Request) ([]fingerprint.Component, error) {
	if s.trusted == nil || !s.trusted(r) {
		return nil, nil
	}
	v := strings.TrimSpace(r.Header.Get(s.header))
	if v == "" {
		return nil, nil
	}
	return []fingerprint.Component{{Name: "tls", Value: v}}, nil
}

type chain struct{ cs []fingerprint.Collector }

// Chain returns a Collector that queries each TLS source in order and returns
// the first non-empty "tls" component.
func Chain(cs ...fingerprint.Collector) fingerprint.Collector { return chain{cs: cs} }

func (c chain) Collect(r *http.Request) ([]fingerprint.Component, error) {
	var firstErr error
	for _, s := range c.cs {
		comps, err := s.Collect(r)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(comps) > 0 {
			return comps, nil
		}
	}
	return nil, firstErr
}
