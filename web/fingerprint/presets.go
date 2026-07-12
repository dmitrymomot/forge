package fingerprint

// Session returns a Fingerprinter for general apps: header and Client Hint
// components only, pure stdlib, no wired seams. Suitable for session-hijack
// detection (persist the Digest, compare with Drift).
func Session(cfg Config, opts ...Option) (*Fingerprinter, error) {
	base := []Option{WithCollectors(Headers(), ClientHints())}
	return New(cfg, append(base, opts...)...)
}

// Antifraud returns a full-stack Fingerprinter: headers + client IP + Client
// Hints + JS probe, with the geoip and useragent seams wired and an optional
// TLS collector (pass tlsprint.Chain(...), or nil to omit the TLS layer).
// Client Hints are included; compose tlsprint.RequestTLS() into the tls
// argument yourself for self-terminated TLS (this package cannot import
// tlsprint). Note: the tls-ua-mismatch signal is inert (always Value:false)
// until WithAutomationJA4 is set.
func Antifraud(cfg Config, geo GeoLookup, ua UAFamily, tls Collector, opts ...Option) (*Fingerprinter, error) {
	cols := []Collector{Headers(), ClientIP(), ClientHints()}
	if tls != nil {
		cols = append(cols, tls)
	}
	base := []Option{WithCollectors(cols...), WithGeoLookup(geo), WithUAFamily(ua)}
	fp, err := New(cfg, append(base, opts...)...)
	if err != nil {
		return nil, err
	}
	fp.cols = append(fp.cols, fp.JSCollector())
	return fp, nil
}
