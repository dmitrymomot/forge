// Package geoip resolves a client IP to geographic and network facts behind a
// pluggable Source seam.
package geoip

// Location holds the facts resolved for a client IP. Every field is its zero
// value when the source could not provide it, so a zero Location means "no
// data" (see Empty).
type Location struct {
	CountryCode string // ISO 3166-1 alpha-2, upper-case ("US"); "" when unknown.
	RegionCode  string // ISO 3166-2 subdivision suffix ("CA").
	RegionName  string // Subdivision name ("California").
	City        string // City name ("San Francisco").
	TimeZone    string // IANA time zone ("America/Los_Angeles").
	ASNOrg      string // Autonomous system organization ("Cloudflare, Inc.").
	ASN         uint32 // Autonomous system number; 0 when unknown.
}

// Empty reports whether no field is populated — a clean "not found".
func (l Location) Empty() bool { return l == Location{} }
