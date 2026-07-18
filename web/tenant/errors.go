package tenant

import "errors"

var (
	// ErrNoTenant is returned by Scope when the context carries no tenant and
	// by Resolver.Resolve when no source yields a live tenant. Fail-closed:
	// callers must not fall back to an unscoped view of the data.
	ErrNoTenant = errors.New("tenant: no tenant in context")

	// ErrTenantNotFound is returned by a Lookup when the identifier maps to
	// no live tenant. The Resolver treats it as "not resolved" and continues
	// with the next source — a request may legitimately carry a non-tenant
	// identifier (the marketing host, an unknown subdomain).
	ErrTenantNotFound = errors.New("tenant: tenant not found")

	// ErrTenantInactive is returned by a Lookup when the tenant exists but
	// must not serve traffic (soft-deleted, disabled, suspended). Resolution
	// fails closed: the request is rejected, never passed through untenanted
	// and never retried against later sources.
	ErrTenantInactive = errors.New("tenant: tenant inactive")
)

// Sentinel errors used as panic payloads by source constructors, options, and
// New when misconfigured. Recover and match them with errors.Is.
var (
	// ErrEmptyName is used when Subdomain, Header, Cookie, or Query is
	// constructed with an empty (after normalization) base domain or name.
	ErrEmptyName = errors.New("tenant: empty base domain or name")
	// ErrNilLookup is used when New is called with a nil Lookup.
	ErrNilLookup = errors.New("tenant: nil lookup")
	// ErrInvalidPrefix is used when PathPrefix is constructed with a prefix
	// that is neither empty nor starting with "/", or that ends with "/".
	ErrInvalidPrefix = errors.New("tenant: invalid path prefix")
	// ErrNilSource is used when WithSources receives a nil Source.
	ErrNilSource = errors.New("tenant: nil source")
	// ErrNoSources is used when New is called without any WithSources option.
	ErrNoSources = errors.New("tenant: no sources configured")
	// ErrNilComponent is used when WithErrorHandler receives nil.
	ErrNilComponent = errors.New("tenant: nil component")
)
