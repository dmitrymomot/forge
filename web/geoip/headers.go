package geoip

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// HeaderMap names the request header each Location field is read from. An empty
// name means the field is not sourced from headers.
type HeaderMap struct {
	Country    string
	RegionCode string
	RegionName string
	City       string
	TimeZone   string
	ASN        string
	ASNOrg     string
}

type headerSource struct{ m HeaderMap }

// Headers returns a Source that maps request headers to Location fields per m.
// Country is upper-cased and validated as two ASCII letters (placeholders like
// Cloudflare's "XX"/"T1" become "" — a miss); City is URL-decoded; ASN is
// parsed as a uint32 (an optional "AS" prefix is stripped). A request missing
// every mapped header yields (Location{}, nil).
func Headers(m HeaderMap) Source { return headerSource{m: m} }

func (s headerSource) Lookup(r *http.Request) (Location, error) {
	h := r.Header
	loc := Location{
		CountryCode: normCountry(get(h, s.m.Country)),
		RegionCode:  get(h, s.m.RegionCode),
		RegionName:  get(h, s.m.RegionName),
		City:        decodeCity(get(h, s.m.City)),
		TimeZone:    get(h, s.m.TimeZone),
		ASN:         parseASN(get(h, s.m.ASN)),
		ASNOrg:      get(h, s.m.ASNOrg),
	}
	return loc, nil
}

func get(h http.Header, name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSpace(h.Get(name))
}

// normCountry returns v upper-cased only when it is exactly two ASCII letters
// and not the "XX" unknown placeholder; anything else becomes "".
func normCountry(v string) string {
	if len(v) != 2 {
		return ""
	}
	v = strings.ToUpper(v)
	for i := range 2 {
		if v[i] < 'A' || v[i] > 'Z' {
			return ""
		}
	}
	if v == "XX" {
		return ""
	}
	return v
}

func decodeCity(v string) string {
	if v == "" {
		return ""
	}
	if dec, err := url.QueryUnescape(v); err == nil {
		return dec
	}
	return v
}

func parseASN(v string) uint32 {
	v = strings.TrimPrefix(v, "AS")
	v = strings.TrimPrefix(v, "as")
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

// Cloudflare reads the free CF-IPCountry header (country only).
func Cloudflare() Source { return Headers(HeaderMap{Country: "CF-IPCountry"}) }

// CloudFront reads the CloudFront-Viewer-* geo headers (enable them on the
// distribution's cache policy).
func CloudFront() Source {
	return Headers(HeaderMap{
		Country:    "CloudFront-Viewer-Country",
		RegionCode: "CloudFront-Viewer-Country-Region",
		RegionName: "CloudFront-Viewer-Country-Region-Name",
		City:       "CloudFront-Viewer-City",
		TimeZone:   "CloudFront-Viewer-Time-Zone",
		ASN:        "CloudFront-Viewer-ASN",
	})
}

// Vercel reads the x-vercel-ip-* geo headers.
func Vercel() Source {
	return Headers(HeaderMap{
		Country:    "x-vercel-ip-country",
		RegionCode: "x-vercel-ip-country-region",
		City:       "x-vercel-ip-city",
		TimeZone:   "x-vercel-ip-timezone",
	})
}
