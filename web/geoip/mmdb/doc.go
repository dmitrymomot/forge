// Package mmdb reads MaxMind-DB-format (.mmdb) geolocation databases and
// implements geoip.Locator. It is a self-contained, reflection-free reader with
// no external dependencies; the database data may come from any vendor that
// publishes the format (DB-IP, MaxMind, IPinfo).
//
// Provide a city/country database and/or an ASN database; a Lookup merges both.
// File paths are memory-mapped on unix (WithInMemory forces a heap read);
// byte slices (go:embed / downloaded) are used in place.
//
//	reader, err := mmdb.New(
//	    mmdb.WithCity("/var/lib/geoip/dbip-city-lite.mmdb"),
//	    mmdb.WithASN("/var/lib/geoip/dbip-asn-lite.mmdb"),
//	)
//	if err != nil { /* ... */ }
//	defer reader.Close()
//	loc, _ := reader.Lookup(ctx, netip.MustParseAddr("81.2.69.142"))
//
// The driver never downloads: fetch and gunzip the .mmdb yourself, then swap in
// a fresh file without a restart by calling Reload on a schedule — e.g. from an
// async/scheduler job that runs monthly:
//
//	func refresh(reader *mmdb.Reader, cityPath, asnPath string) error {
//	    return reader.Reload(mmdb.WithCity(cityPath), mmdb.WithASN(asnPath))
//	}
package mmdb
