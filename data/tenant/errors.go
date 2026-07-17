package tenant

import "errors"

var (
	// ErrNoTenant is returned by Scope and ScopeClause when the context
	// carries no tenant. Fail-closed: callers must not fall back to an
	// unscoped query.
	ErrNoTenant = errors.New("tenant: no tenant in context")

	// ErrTenantNotFound is returned by a DomainLookup or SubdomainLookup
	// (and by Map translation funcs) when the alias maps to no tenant. The
	// resolver treats it as "not resolved" and the middleware continues
	// with the next resolver; any other lookup error stops the chain.
	ErrTenantNotFound = errors.New("tenant: tenant not found")

	// ErrInvalidColumn rejects a ScopeClause column that is not a plain,
	// optionally qualified SQL identifier.
	ErrInvalidColumn = errors.New("tenant: invalid column identifier")

	// ErrInvalidPlaceholder rejects a ScopeClause placeholder that is
	// neither "?" nor "$n".
	ErrInvalidPlaceholder = errors.New("tenant: invalid placeholder")
)

// Sentinel errors used as panic payloads by resolver constructors and
// Middleware when misconfigured. Recover and match them with errors.Is.
var (
	// ErrEmptyName is used when Subdomain, Header, or Cookie is constructed
	// with an empty (after normalization) base domain or name.
	ErrEmptyName = errors.New("tenant: empty base domain or name")
	// ErrNilLookup is used when Domain or Subdomain is constructed with a
	// nil lookup, or Map with a nil translation func.
	ErrNilLookup = errors.New("tenant: nil lookup")
	// ErrInvalidPrefix is used when PathPrefix is constructed with a prefix
	// that is neither empty nor starting with "/", or that ends with "/".
	ErrInvalidPrefix = errors.New("tenant: invalid path prefix")
	// ErrNilResolver is used when Middleware is constructed with a nil Resolver.
	ErrNilResolver = errors.New("tenant: nil resolver")
)
