package geoip_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/geoip"
)

func req(h map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range h {
		r.Header.Set(k, v)
	}
	return r
}

func TestCloudflare(t *testing.T) {
	loc, err := geoip.Cloudflare().Lookup(req(map[string]string{"CF-IPCountry": "us"}))
	if err != nil {
		t.Fatal(err)
	}
	if loc.CountryCode != "US" {
		t.Fatalf("country = %q, want US", loc.CountryCode)
	}
}

func TestCloudflarePlaceholdersAreMiss(t *testing.T) {
	for _, v := range []string{"XX", "T1", "", "USA"} {
		loc, _ := geoip.Cloudflare().Lookup(req(map[string]string{"CF-IPCountry": v}))
		if !loc.Empty() {
			t.Fatalf("CF-IPCountry=%q should be a miss, got %+v", v, loc)
		}
	}
}

func TestCloudFrontRichAndNormalized(t *testing.T) {
	loc, _ := geoip.CloudFront().Lookup(req(map[string]string{
		"CloudFront-Viewer-Country":             "de",
		"CloudFront-Viewer-Country-Region":      "BE",
		"CloudFront-Viewer-Country-Region-Name": "Berlin",
		"CloudFront-Viewer-City":                "Berlin",
		"CloudFront-Viewer-Time-Zone":           "Europe/Berlin",
		"CloudFront-Viewer-ASN":                 "3320",
	}))
	want := geoip.Location{
		CountryCode: "DE", RegionCode: "BE", RegionName: "Berlin",
		City: "Berlin", TimeZone: "Europe/Berlin", ASN: 3320,
	}
	if loc != want {
		t.Fatalf("got %+v, want %+v", loc, want)
	}
}

func TestVercelURLDecodesCity(t *testing.T) {
	loc, _ := geoip.Vercel().Lookup(req(map[string]string{
		"x-vercel-ip-country": "US",
		"x-vercel-ip-city":    "San%20Francisco",
	}))
	if loc.City != "San Francisco" {
		t.Fatalf("city = %q, want %q", loc.City, "San Francisco")
	}
}

func TestHeadersASNPrefixStripped(t *testing.T) {
	loc, _ := geoip.Headers(geoip.HeaderMap{ASN: "X-ASN"}).Lookup(req(map[string]string{"X-ASN": "AS13335"}))
	if loc.ASN != 13335 {
		t.Fatalf("asn = %d, want 13335", loc.ASN)
	}
}
