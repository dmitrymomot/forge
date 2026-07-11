package mmdb

import (
	"net/netip"
	"os"
	"testing"
)

func FuzzParseAndLookup(f *testing.F) {
	if seed, err := os.ReadFile("testdata/GeoIP2-City-Test.mmdb"); err == nil {
		f.Add(seed)
	}
	if seed, err := os.ReadFile("testdata/GeoLite2-ASN-Test.mmdb"); err == nil {
		f.Add(seed)
	}
	f.Add([]byte("not a database"))
	f.Add([]byte{})

	ip := netip.MustParseAddr("81.2.69.142")
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must never panic or hang on arbitrary bytes.
		db, err := parseMetadata(data, func() error { return nil })
		if err != nil {
			return
		}
		off, ok := db.lookupOffset(ip)
		if !ok {
			return
		}
		_, _ = db.decodeLocation(off)
	})
}
