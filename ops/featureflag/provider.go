package featureflag

import "context"

// Provider supplies flag definitions. Implementations must be safe for
// concurrent use. A miss is (Flag{}, false, nil). The client treats an error
// as a miss for that provider (logs it and consults the next one), so
// evaluation stays fail-safe.
//
// The ctx parameter is how multi-tenancy works: a database-backed provider
// reads the tenant ID from request context and keys its lookup on
// (tenant, key). The core package never learns about tenants.
type Provider interface {
	Flag(ctx context.Context, key string) (Flag, bool, error)
}

// Lister is an optional Provider upgrade for debug/admin visibility.
// Client.All merges results across providers that implement it.
type Lister interface {
	All(ctx context.Context) (Flags, error)
}
