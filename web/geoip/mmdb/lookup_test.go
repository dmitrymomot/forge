package mmdb_test

import (
	"context"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/web/geoip/mmdb"
)

func TestLookupCity(t *testing.T) {
	r, err := mmdb.New(mmdb.WithCity("testdata/GeoIP2-City-Test.mmdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	loc, err := r.Lookup(context.Background(), netip.MustParseAddr("81.2.69.142"))
	if err != nil {
		t.Fatal(err)
	}
	// Canonical MaxMind test-data values for 81.2.69.142. If a string differs,
	// verify against the fixture's companion JSON in maxmind/MaxMind-DB
	// (source-data/) and adjust.
	if loc.CountryCode != "GB" {
		t.Fatalf("country = %q, want GB", loc.CountryCode)
	}
	if loc.City != "London" {
		t.Fatalf("city = %q, want London", loc.City)
	}
	if loc.TimeZone != "Europe/London" {
		t.Fatalf("tz = %q, want Europe/London", loc.TimeZone)
	}
	if loc.RegionName != "England" {
		t.Fatalf("region = %q, want England", loc.RegionName)
	}
}

func TestLookupASN(t *testing.T) {
	r, err := mmdb.New(mmdb.WithASN("testdata/GeoLite2-ASN-Test.mmdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	loc, err := r.Lookup(context.Background(), netip.MustParseAddr("1.128.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.ASN != 1221 {
		t.Fatalf("asn = %d, want 1221", loc.ASN)
	}
	if loc.ASNOrg == "" {
		t.Fatal("asn org should be populated")
	}
}

func TestLookupMissIsEmptyNoError(t *testing.T) {
	r, _ := mmdb.New(mmdb.WithCity("testdata/GeoIP2-City-Test.mmdb"))
	defer r.Close()
	loc, err := r.Lookup(context.Background(), netip.MustParseAddr("203.0.113.1"))
	if err != nil || !loc.Empty() {
		t.Fatalf("got (%+v, %v), want empty + nil", loc, err)
	}
}

func TestReloadSwapsData(t *testing.T) {
	r, _ := mmdb.New(mmdb.WithCity("testdata/GeoIP2-City-Test.mmdb"))
	defer r.Close()
	if err := r.Reload(mmdb.WithCity("testdata/GeoIP2-City-Test.mmdb"), mmdb.WithASN("testdata/GeoLite2-ASN-Test.mmdb")); err != nil {
		t.Fatal(err)
	}
	loc, err := r.Lookup(context.Background(), netip.MustParseAddr("1.128.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.ASN != 1221 {
		t.Fatalf("after reload asn = %d, want 1221", loc.ASN)
	}
}
