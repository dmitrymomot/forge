package tenant

import "errors"

var (
	// ErrNoTenant is returned by Scope when the context carries no tenant and
	// by Resolver.Resolve when no source yields an identifier. Fail-closed:
	// callers must not fall back to an unscoped view of the data.
	ErrNoTenant = errors.New("tenant: no tenant in context")

	// ErrTenantNotFound is returned by a DomainLookup or SubdomainLookup (and
	// by Map translation funcs) when an alias maps to no tenant — the source
	// treats it as "not resolved" and the chain continues — and by a Validator
	// when a resolved ID identifies no tenant, which rejects the request.
	ErrTenantNotFound = errors.New("tenant: tenant not found")

	// ErrTenantInactive is returned by a Validator when the tenant exists but
	// must not serve traffic (soft-deleted, disabled, suspended). Resolution
	// fails closed: the request is rejected, never passed through untenanted.
	ErrTenantInactive = errors.New("tenant: tenant inactive")
)

// Sentinel errors used as panic payloads by source constructors, options, and
// New when misconfigured. Recover and match them with errors.Is.
var (
	// ErrEmptyName is used when Subdomain, Header, Cookie, or Query is
	// constructed with an empty (after normalization) base domain or name.
	ErrEmptyName = errors.New("tenant: empty base domain or name")
	// ErrNilLookup is used when Domain or Subdomain is constructed with a nil
	// lookup, or Map with a nil translation func.
	ErrNilLookup = errors.New("tenant: nil lookup")
	// ErrInvalidPrefix is used when PathPrefix is constructed with a prefix
	// that is neither empty nor starting with "/", or that ends with "/".
	ErrInvalidPrefix = errors.New("tenant: invalid path prefix")
	// ErrNilSource is used when WithSources or Map receives a nil Source.
	ErrNilSource = errors.New("tenant: nil source")
	// ErrNoSources is used when New is called without any WithSources option.
	ErrNoSources = errors.New("tenant: no sources configured")
	// ErrNilComponent is used when WithValidator or WithErrorHandler receives nil.
	ErrNilComponent = errors.New("tenant: nil component")
)
