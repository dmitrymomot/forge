package mmdb_test

import (
	"context"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/web/geoip/mmdb"
)

func BenchmarkLookup(b *testing.B) {
	r, err := mmdb.New(mmdb.WithCity("testdata/GeoIP2-City-Test.mmdb"), mmdb.WithASN("testdata/GeoLite2-ASN-Test.mmdb"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	ip := netip.MustParseAddr("81.2.69.142")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Lookup(ctx, ip); err != nil {
			b.Fatal(err)
		}
	}
}
