package tenant

import (
	"context"
	"errors"
	"net/http"
	"net/textproto"
	"strings"
)

// Resolver extracts a tenant identifier from an HTTP request. Returning
// ("", nil) means "not resolved" and the middleware tries the next resolver
// in the chain; a non-empty ID resolves the request; a non-nil error stops
// the chain and the middleware responds 500.
//
// Resolvers return the raw identifier they saw (subdomain label, header
// value, path segment). A consumer that keys tenants on something else wraps
// a Resolver — it is a plain func — or maps inside its DomainLookup.
type Resolver func(r *http.Request) (string, error)

// DomainLookup maps a full custom domain to a tenant ID. Implementations
// return ErrDomainNotFound when the domain maps to no tenant; any other
// error is treated as infrastructure failure and stops resolution.
type DomainLookup interface {
	TenantByDomain(ctx context.Context, domain string) (string, error)
}

// Subdomain resolves the tenant from the first DNS label in front of base:
// with base "app.example.com", host "acme.app.example.com" resolves "acme".
// The bare base and nested labels ("a.b.app.example.com") do not resolve.
// Hosts are compared case-insensitively with any port, IPv6 brackets, and
// trailing FQDN dot stripped.
//
// Reserved names (www, api, ...) are not special-cased: exclude them in your
// handler or by wrapping the returned Resolver. Panics with ErrEmptyName
// when base normalizes to "".
func Subdomain(base string) Resolver {
	base = normalizeHost(base)
	if base == "" {
		panic(ErrEmptyName)
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
		return label, nil
	}
}

// Domain resolves the tenant by looking up the full (normalized) request
// host through lookup — the custom-domain path of a white-label SaaS.
// ErrDomainNotFound from the lookup means "not resolved" and the chain
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
			if errors.Is(err, ErrDomainNotFound) {
				return "", nil
			}
			return "", err
		}
		return id, nil
	}
}

// Header resolves the tenant from a request header, e.g. "X-Tenant-ID".
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

// Cookie resolves the tenant from a cookie value. Like Header, the value is
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
// PathPrefix("/t") resolves "acme" from "/t/acme/dashboard", and
// PathPrefix("") resolves the first segment ("/acme/dashboard"). The path is
// never rewritten — stripping the prefix stays a routing concern. prefix
// must be empty or start with "/" and not end with "/"; otherwise the
// constructor panics with ErrInvalidPrefix.
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

// staticDomains is the in-memory DomainLookup for tests and development.
type staticDomains map[string]string

// StaticDomains returns an immutable in-memory DomainLookup for tests and
// development. Keys are normalized like request hosts (lowercased, port and
// trailing dot stripped); entries with an empty tenant ID are dropped.
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
	return "", ErrDomainNotFound
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
