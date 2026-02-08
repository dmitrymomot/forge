package geolocation

import (
	"context"
	"strings"
)

// Location represents geographic information resolved from an IP address.
type Location struct {
	// Country is the ISO 3166-1 alpha-2 country code (e.g., "US", "GB", "DE").
	Country string

	// City is the English name of the city (e.g., "London", "New York").
	City string

	// Region is the English name of the primary subdivision,
	// typically a state or province (e.g., "California", "England").
	Region string

	// Timezone is the IANA timezone identifier (e.g., "America/New_York", "Europe/London").
	Timezone string
}

// String returns a human-readable representation of the location.
// Empty components are omitted. Examples:
//   - "London, England, GB"
//   - "California, US" (no city)
//   - "US" (only country)
//   - "" (all empty)
func (l *Location) String() string {
	parts := make([]string, 0, 3)
	if l.City != "" {
		parts = append(parts, l.City)
	}
	if l.Region != "" {
		parts = append(parts, l.Region)
	}
	if l.Country != "" {
		parts = append(parts, l.Country)
	}
	return strings.Join(parts, ", ")
}

// Provider abstracts IP geolocation lookups.
// Implementations must be safe for concurrent use.
type Provider interface {
	// Lookup resolves the geographic location of the given IP address.
	// Returns (nil, nil) for private, loopback, link-local, and unspecified IPs.
	// Returns ErrInvalidIP if the IP string cannot be parsed.
	// Returns ErrClosed if the provider has been closed.
	Lookup(ctx context.Context, ip string) (*Location, error)

	// Close releases resources held by the provider.
	// After Close returns, subsequent Lookup calls return ErrClosed.
	Close() error
}
