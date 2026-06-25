// Package geolocation provides IP geolocation lookups via MaxMind GeoIP2/GeoLite2 databases.
//
// It defines a [Provider] interface for pluggable backends and ships a
// [MaxMindProvider] that memory-maps a MaxMind database file for
// high-performance, thread-safe lookups.
//
// # Usage
//
//	provider, err := geolocation.NewMaxMindProvider("/path/to/GeoLite2-City.mmdb")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Close()
//
//	loc, err := provider.Lookup(ctx, "81.2.69.142")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if loc != nil {
//	    fmt.Println(loc)           // "London, England, GB"
//	    fmt.Println(loc.Country)   // "GB"
//	    fmt.Println(loc.Timezone)  // "Europe/London"
//	}
//
// # Nil Results
//
// Lookups for private (RFC 1918/4193), loopback, link-local, and unspecified
// addresses return (nil, nil) instead of an error. This allows middleware
// and application code to handle local development gracefully:
//
//	loc, err := provider.Lookup(ctx, "127.0.0.1")
//	// loc == nil, err == nil
//
//	loc, err = provider.Lookup(ctx, "192.168.1.1")
//	// loc == nil, err == nil
//
// A routable public IP that has no matching record in the database also
// returns (nil, nil), so callers must always check loc != nil before
// dereferencing it:
//
//	loc, err = provider.Lookup(ctx, "8.8.8.8")
//	// loc may be nil if the IP is absent from the database
//
// # Error Handling
//
// The package defines sentinel errors for common failure modes:
//
//   - [ErrClosed] — operation on a closed provider
//   - [ErrInvalidIP] — IP string could not be parsed
//
// Use [errors.Is] for checking:
//
//	if errors.Is(err, geolocation.ErrClosed) {
//	    // provider was closed
//	}
//
// # Integration with clientip
//
// Combine with [github.com/dmitrymomot/forge/pkg/clientip] to extract
// the client IP from an HTTP request and geolocate it:
//
//	ip := clientip.GetIP(r)
//	loc, err := provider.Lookup(ctx, ip)
package geolocation
