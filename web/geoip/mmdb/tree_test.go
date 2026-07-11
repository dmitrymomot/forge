package mmdb

import (
	"net/netip"
	"testing"
)

func TestLookupOffsetFindsKnownIP(t *testing.T) {
	db := loadDB(t, "GeoIP2-City-Test.mmdb")
	off, ok := db.lookupOffset(netip.MustParseAddr("81.2.69.142"))
	if !ok {
		t.Fatal("81.2.69.142 should be found in the city test DB")
	}
	if off < int(db.dataStart) || off >= len(db.data) {
		t.Fatalf("data offset %d out of range", off)
	}
}

func TestLookupOffsetMiss(t *testing.T) {
	db := loadDB(t, "GeoIP2-City-Test.mmdb")
	// 203.0.113.0/24 (TEST-NET-3) is not in the fixture.
	if _, ok := db.lookupOffset(netip.MustParseAddr("203.0.113.1")); ok {
		t.Fatal("TEST-NET-3 address should miss")
	}
}
