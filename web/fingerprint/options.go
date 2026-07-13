package fingerprint

import (
	"log/slog"
	"maps"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// Option configures a Fingerprinter in New.
type Option func(*Fingerprinter)

// WithCollectors appends layers (headers, IP, TLS, JS) to the Fingerprinter.
func WithCollectors(cs ...Collector) Option {
	return func(fp *Fingerprinter) { fp.cols = append(fp.cols, cs...) }
}

// WithGeoLookup wires the geoip seam, enabling datacenter-asn and geo-tz-mismatch.
func WithGeoLookup(fn GeoLookup) Option { return func(fp *Fingerprinter) { fp.geo = fn } }

// WithUAFamily wires the useragent seam, enabling bot-ua, tls-ua-mismatch, header-anomaly.
func WithUAFamily(fn UAFamily) Option { return func(fp *Fingerprinter) { fp.ua = fn } }

// WithStore backs JS-probe payload correlation with a cache.Store instead of a
// payload-carrying cookie (the cookie then carries only the nonce).
func WithStore(s cache.Store) Option { return func(fp *Fingerprinter) { fp.store = s } }

// WithLogger sets the logger for best-effort Debug messages. A nil logger is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(fp *Fingerprinter) {
		if l != nil {
			fp.logger = l
		}
	}
}

// WithClock overrides the clock used for probe-token expiry (tests inject a mock).
func WithClock(c clock.Clock) Option {
	return func(fp *Fingerprinter) {
		if c != nil {
			fp.clock = c
		}
	}
}

// WithAutomationJA4 pins non-browser JA4 client fingerprints to labels so the
// tls-ua-mismatch signal fires (Value:true) when the "tls" component matches a
// pinned fingerprint under a browser-family UA. Ships empty by design — pinned
// TLS fingerprints drift as tools update, so populate this from fingerprints you
// capture from your own traffic (see the tlsprint.Listener / Conn.JA4() capture
// recipe in the package doc). The map is cloned.
func WithAutomationJA4(m map[string]string) Option {
	return func(fp *Fingerprinter) { fp.automationJA4 = maps.Clone(m) }
}
