package geolocation

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github.com/oschwald/geoip2-golang/v2"
)

// MaxMindProvider performs IP geolocation lookups using a MaxMind
// GeoIP2 or GeoLite2 database. It memory-maps the database file
// for efficient, zero-copy reads.
//
// MaxMindProvider is safe for concurrent use. Multiple goroutines
// may call Lookup simultaneously.
type MaxMindProvider struct {
	db     *geoip2.Reader
	mu     sync.RWMutex
	closed bool
}

// NewMaxMindProvider opens a MaxMind GeoIP2 or GeoLite2 database file
// and returns a provider ready for lookups.
//
// The database file is memory-mapped for performance. The file must
// remain accessible on disk for the lifetime of the provider.
//
// Call Close when the provider is no longer needed to release resources.
func NewMaxMindProvider(dbPath string) (*MaxMindProvider, error) {
	db, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("geolocation: open database: %w", err)
	}
	return &MaxMindProvider{db: db}, nil
}

// Lookup resolves the geographic location for the given IP address.
//
// For private (RFC 1918/4193), loopback, link-local, and unspecified
// addresses, Lookup returns (nil, nil) to support graceful degradation
// in development and internal-network environments.
//
// Lookup also returns (nil, nil) for a routable public IP that has no
// matching record in the database, so callers must check loc != nil
// before dereferencing it.
//
// Returns ErrInvalidIP if the IP string cannot be parsed.
// Returns ErrClosed if Close has been called.
func (p *MaxMindProvider) Lookup(_ context.Context, ip string) (*Location, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, ErrClosed
	}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidIP, ip)
	}

	if isNonRoutable(addr) {
		return nil, nil
	}

	record, err := p.db.City(addr)
	if err != nil {
		return nil, fmt.Errorf("geolocation: lookup failed: %w", err)
	}

	if !record.HasData() {
		return nil, nil
	}

	loc := &Location{
		Country:  record.Country.ISOCode,
		City:     record.City.Names.English,
		Timezone: record.Location.TimeZone,
	}

	if len(record.Subdivisions) > 0 {
		loc.Region = record.Subdivisions[0].Names.English
	}

	return loc, nil
}

// Close releases the memory-mapped database and marks the provider as closed.
// After Close returns, all subsequent Lookup calls return ErrClosed.
// Close is idempotent.
func (p *MaxMindProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	if err := p.db.Close(); err != nil {
		return fmt.Errorf("geolocation: close database: %w", err)
	}
	return nil
}

// isNonRoutable reports whether addr is private, loopback,
// link-local, or unspecified (e.g., 0.0.0.0, ::).
func isNonRoutable(addr netip.Addr) bool {
	return addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsUnspecified()
}

// Compile-time interface check.
var _ Provider = (*MaxMindProvider)(nil)
