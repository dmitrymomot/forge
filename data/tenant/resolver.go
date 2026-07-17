package tenant

import (
	"context"
	"errors"
	"net/http"
	"net/textproto"
	"strings"
)

// Resolver resolves the canonical tenant ID (uuid/ulid/short id — whatever
// the consumer's tenants table keys on) from an HTTP request. Returning
// ("", nil) means "not resolved" and the middleware tries the next resolver
// in the chain; a non-empty ID resolves the request; a non-nil error stops
// the chain and the middleware responds 500.
//
// A subdomain label, custom domain, or URL slug is an alias, never the ID:
// Subdomain and Domain translate aliases through their lookup seams, and Map
// decorates any other Resolver with the same translation step.
type Resolver func(r *http.Request) (string, error)

// DomainLookup maps a full custom domain to a tenant ID. Implementations
// return ErrTenantNotFound when the domain maps to no tenant; any other
// error is treated as infrastructure failure and stops resolution.
type DomainLookup interface {
	TenantByDomain(ctx context.Context, domain string) (string, error)
}

// SubdomainLookup maps a subdomain label ("acme") to a tenant ID.
// Implementations return ErrTenantNotFound when the label maps to no
// tenant; any other error is treated as infrastructure failure and stops
// resolution.
type SubdomainLookup interface {
	TenantBySubdomain(ctx context.Context, subdomain string) (string, error)
}

// Subdomain resolves the tenant from the first DNS label in front of base,
// translated to a tenant ID through lookup: with base "app.example.com",
// host "acme.app.example.com" looks up "acme". The bare base and nested
// labels ("a.b.app.example.com") do not resolve. Hosts are compared
// case-insensitively with any port, IPv6 brackets, and trailing FQDN dot
// stripped.
//
// Reserved names (www, api, ...) are not special-cased — the lookup decides:
// return ErrTenantNotFound for them and the chain continues. Panics with
// ErrEmptyName when base normalizes to "" and with ErrNilLookup when lookup
// is nil.
func Subdomain(base string, lookup SubdomainLookup) Resolver {
	base = normalizeHost(base)
	if base == "" {
		panic(ErrEmptyName)
	}
	if lookup == nil {
		panic(ErrNilLookup)
	}
	return func(r *http.Request) (string, error) {
		host := normalizeHost(r.Host)
		rest, ok := strings.CutSuffix(host, base)
		if !ok || len(rest) < 2 || rest[len(rest)-1] != '.' {
			return "", nil
		}
		label := rest[:len(rest)-1]
		if strings.IndexByte(label, '.') >= 0 {
			return "", nil
		}
		id, err := lookup.TenantBySubdomain(r.Context(), label)
		if err != nil {
			if errors.Is(err, ErrTenantNotFound) {
				return "", nil
			}
			return "", err
		}
		return id, nil
	}
}

// Domain resolves the tenant by looking up the full (normalized) request
// host through lookup — the custom-domain path of a white-label SaaS.
// ErrTenantNotFound from the lookup means "not resolved" and the chain
// continues; any other error stops the chain. Panics with ErrNilLookup when
// lookup is nil.
func Domain(lookup DomainLookup) Resolver {
	if lookup == nil {
		panic(ErrNilLookup)
	}
	return func(r *http.Request) (string, error) {
		host := normalizeHost(r.Host)
		if host == "" {
			return "", nil
		}
		id, err := lookup.TenantByDomain(r.Context(), host)
		if err != nil {
			if errors.Is(err, ErrTenantNotFound) {
				return "", nil
			}
			return "", err
		}
		return id, nil
	}
}

// Header resolves the tenant from a request header, e.g. "X-Tenant-ID".
// The header is expected to carry the tenant ID itself; wrap with Map when
// it carries an alias instead.
//
// The value is attacker-controlled on any edge-facing listener: use this
// resolver only behind a trusted gateway that sets or strips the header, or
// for internal service-to-service traffic. Panics with ErrEmptyName when
// name is empty.
func Header(name string) Resolver {
	if name == "" {
		panic(ErrEmptyName)
	}
	// Pre-canonicalize so Header.Get's own canonicalization hits its no-alloc
	// fast path on every request ("X-Tenant-ID" would otherwise allocate as
	// its canonical form is "X-Tenant-Id").
	name = textproto.CanonicalMIMEHeaderKey(name)
	return func(r *http.Request) (string, error) {
		return r.Header.Get(name), nil
	}
}

// Cookie resolves the tenant from a cookie expected to carry the tenant ID
// itself (wrap with Map for aliases). Like Header, the value is
// client-controlled — pair it with authorization checks downstream. Panics
// with ErrEmptyName when name is empty.
func Cookie(name string) Resolver {
	if name == "" {
		panic(ErrEmptyName)
	}
	return func(r *http.Request) (string, error) {
		c, err := r.Cookie(name)
		if err != nil { // only http.ErrNoCookie is possible
			return "", nil
		}
		return c.Value, nil
	}
}

// PathPrefix resolves the tenant from the path segment following prefix:
// PathPrefix("/t") resolves "t_123" from "/t/t_123/dashboard", and
// PathPrefix("") resolves the first segment. The segment is expected to
// carry the tenant ID itself; wrap with Map when your URLs carry slugs. The
// path is never rewritten — stripping the prefix stays a routing concern.
// prefix must be empty or start with "/" and not end with "/"; otherwise
// the constructor panics with ErrInvalidPrefix.
func PathPrefix(prefix string) Resolver {
	if prefix != "" && (prefix[0] != '/' || prefix[len(prefix)-1] == '/') {
		panic(ErrInvalidPrefix)
	}
	return func(r *http.Request) (string, error) {
		rest, ok := strings.CutPrefix(r.URL.Path, prefix)
		if !ok || len(rest) < 2 || rest[0] != '/' {
			return "", nil
		}
		seg := rest[1:]
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		return seg, nil
	}
}

// Context resolves a tenant already stamped on the request context by an
// upstream middleware — e.g. API-key authentication that called NewContext
// after verifying the key. Middleware overrides any pre-existing context
// tenant, so this resolver is how a context-derived tenant gets an explicit
// slot in the precedence order.
func Context() Resolver {
	return func(r *http.Request) (string, error) {
		id, _ := FromContext(r.Context())
		return id, nil
	}
}

// Map decorates r, translating every value it resolves through fn — the
// alias→tenant-ID step for resolvers that see aliases the shipped lookups
// don't cover, e.g. a slug in the URL path:
//
//	tenant.Map(tenant.PathPrefix("/t"), func(ctx context.Context, slug string) (string, error) {
//		return tenants.IDBySlug(ctx, slug) // ErrTenantNotFound to continue the chain
//	})
//
// fn runs only when r resolved a non-empty value; ErrTenantNotFound from fn
// means "not resolved" and the chain continues, any other error stops the
// chain. Panics with ErrNilResolver when r is nil and ErrNilLookup when fn
// is nil.
func Map(r Resolver, fn func(ctx context.Context, value string) (string, error)) Resolver {
	if r == nil {
		panic(ErrNilResolver)
	}
	if fn == nil {
		panic(ErrNilLookup)
	}
	return func(req *http.Request) (string, error) {
		value, err := r(req)
		if err != nil || value == "" {
			return "", err
		}
		id, err := fn(req.Context(), value)
		if err != nil {
			if errors.Is(err, ErrTenantNotFound) {
				return "", nil
			}
			return "", err
		}
		return id, nil
	}
}

// staticDomains is the in-memory DomainLookup for tests and development.
type staticDomains map[string]string

// StaticDomains returns an immutable in-memory DomainLookup for tests and
// development, mapping custom domains to tenant IDs. Keys are normalized
// like request hosts (lowercased, port and trailing dot stripped); entries
// with an empty tenant ID are dropped.
func StaticDomains(domains map[string]string) DomainLookup {
	s := make(staticDomains, len(domains))
	for domain, id := range domains {
		if d := normalizeHost(domain); d != "" && id != "" {
			s[d] = id
		}
	}
	return s
}

func (s staticDomains) TenantByDomain(_ context.Context, domain string) (string, error) {
	if id, ok := s[normalizeHost(domain)]; ok {
		return id, nil
	}
	return "", ErrTenantNotFound
}

// staticSubdomains is the in-memory SubdomainLookup for tests and development.
type staticSubdomains map[string]string

// StaticSubdomains returns an immutable in-memory SubdomainLookup for tests
// and development, mapping subdomain labels to tenant IDs. Keys are
// lowercased to match the normalized labels the Subdomain resolver looks
// up; entries with an empty key or tenant ID are dropped.
func StaticSubdomains(subdomains map[string]string) SubdomainLookup {
	s := make(staticSubdomains, len(subdomains))
	for label, id := range subdomains {
		if label != "" && id != "" {
			s[strings.ToLower(label)] = id
		}
	}
	return s
}

func (s staticSubdomains) TenantBySubdomain(_ context.Context, subdomain string) (string, error) {
	if id, ok := s[subdomain]; ok {
		return id, nil
	}
	return "", ErrTenantNotFound
}

// normalizeHost lowercases the host, strips any port, removes IPv6 brackets,
// and trims a trailing FQDN dot. It allocates only on strings.ToLower's slow
// path (uppercase input); an already-lowercase host returns sub-slices with
// no copy.
//
// net.SplitHostPort is deliberately avoided: it allocates an *AddrError
// whenever the host has no port (the common proxied/HTTP2 case).
func normalizeHost(host string) string {
	if host == "" {
		return ""
	}
	if host[0] == '[' { // IPv6 literal: "[::1]" or "[::1]:8080"
		i := strings.IndexByte(host, ']')
		if i < 0 {
			return "" // malformed: opening '[' with no closing ']'
		}
		host = host[1:i] // inside brackets; drops "]" and any ":port" after it
	} else if i := strings.LastIndexByte(host, ':'); i >= 0 &&
		strings.IndexByte(host, ':') == i {
		host = host[:i] // exactly one colon => host:port (not bracketless IPv6)
	}
	host = strings.TrimSuffix(host, ".") // rooted FQDN "example.com."
	return strings.ToLower(host)
}
