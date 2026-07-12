package fingerprint

import "net/netip"

// GeoInfo is the subset of geoip facts the signal inspectors need. Wire it from
// web/geoip (see example_test.go). Covers datacenter-asn and geo-tz-mismatch.
type GeoInfo struct {
	Continent string // two-letter continent code ("EU", "NA", "AS", ...)
	Timezone  string // IANA zone of the IP ("Europe/Berlin"), optional
	ASN       uint
	Hosting   bool // datacenter/hosting/VPN ASN
}

// GeoLookup resolves a client IP to GeoInfo. ok is false on a clean miss.
type GeoLookup func(netip.Addr) (GeoInfo, bool)

// Family is a coarse client class inferred from the User-Agent.
type Family int

const (
	FamilyUnknown Family = iota
	FamilyBrowser
	FamilyBot
)

// UAFamily classifies a User-Agent string. Wire it from web/useragent.
type UAFamily func(ua string) (Family, bool)
