package mmdb_test

import (
	"context"
	"net/netip"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/web/geoip/mmdb"
)

// TestConcurrentLookupDuringReload exercises the Reader's headline invariant:
// Lookup must be safe to call concurrently with Reload, which atomically
// swaps the underlying databases and munmaps the old ones. The whole point
// of this test is to be run under `go test -race`; it asserts no panic, no
// error, and (via -race) no data race on the swapped database pointers.
func TestConcurrentLookupDuringReload(t *testing.T) {
	r, err := mmdb.New(
		mmdb.WithCity("testdata/GeoIP2-City-Test.mmdb"),
		mmdb.WithASN("testdata/GeoLite2-ASN-Test.mmdb"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	cityIP := netip.MustParseAddr("81.2.69.142")
	asnIP := netip.MustParseAddr("1.128.0.0")

	const lookupGoroutines = 8
	const lookupIterations = 300
	const reloadIterations = 300

	var wg sync.WaitGroup

	for range lookupGoroutines {
		wg.Go(func() {
			for range lookupIterations {
				if _, err := r.Lookup(context.Background(), cityIP); err != nil {
					t.Errorf("Lookup(city) error: %v", err)
				}
				if _, err := r.Lookup(context.Background(), asnIP); err != nil {
					t.Errorf("Lookup(asn) error: %v", err)
				}
			}
		})
	}

	wg.Go(func() {
		for range reloadIterations {
			if err := r.Reload(
				mmdb.WithCity("testdata/GeoIP2-City-Test.mmdb"),
				mmdb.WithASN("testdata/GeoLite2-ASN-Test.mmdb"),
			); err != nil {
				t.Errorf("Reload error: %v", err)
			}
		}
	})

	wg.Wait()
}
