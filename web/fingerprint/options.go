package fingerprint

import (
	"log/slog"

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
