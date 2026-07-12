package fingerprint

// Session returns a Fingerprinter for general apps: header components only, pure
// stdlib, no wired seams. Suitable for session-hijack detection (persist the
// Digest, compare with Drift).
func Session(cfg Config, opts ...Option) (*Fingerprinter, error) {
	base := []Option{WithCollectors(Headers())}
	return New(cfg, append(base, opts...)...)
}

// Antifraud returns a full-stack Fingerprinter: headers + client IP + JS probe,
// with the geoip and useragent seams wired and an optional TLS collector (pass
// tlsprint.Chain(...), or nil to omit the TLS layer).
func Antifraud(cfg Config, geo GeoLookup, ua UAFamily, tls Collector, opts ...Option) (*Fingerprinter, error) {
	cols := []Collector{Headers(), ClientIP()}
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
