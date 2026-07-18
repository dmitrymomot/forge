package tenant

import (
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// Kind names the namespace an extracted identifier value lives in, so one
// Lookup implementation can interpret every source: a subdomain label is not
// a tenant ID is not a custom domain. Kind is a plain string type — a custom
// Source may invent its own kind and handle it in the consumer's Lookup.
type Kind string

const (
	// KindID marks a value expected to be the canonical tenant ID itself
	// (uuid/ulid/short id — whatever the consumer's tenants table keys on).
	// Yielded by Header, Cookie, Query, and Context.
	KindID Kind = "id"
	// KindDomain marks a full custom domain ("shop.acme.com"). Yielded by
	// Domain.
	KindDomain Kind = "domain"
	// KindSubdomain marks a single subdomain label ("acme"). Yielded by
	// Subdomain.
	KindSubdomain Kind = "subdomain"
	// KindPath marks a URL path segment — a slug or an ID, whichever the
	// consumer's URL scheme puts there. Yielded by PathPrefix.
	KindPath Kind = "path"
)

// Identifier is a candidate tenant identifier extracted from a request:
// a raw value tagged with the namespace it lives in. The Lookup seam
// translates it to a live canonical tenant ID.
type Identifier struct {
	Kind  Kind
	Value string
}

// Source extracts a candidate tenant identifier from a request. ok=false
// means "not present" — the Resolver tries the next source in the chain; it
// never means "present but bad" (that judgment belongs to the Lookup).
// Sources are pure: no I/O, no errors — all knowledge of what exists lives
// behind the Lookup seam.
type Source func(r *http.Request) (ident Identifier, ok bool)

// Subdomain extracts the first DNS label in front of base as KindSubdomain:
// with base "app.example.com", host "acme.app.example.com" yields "acme".
// The bare base and nested labels ("a.b.app.example.com") do not extract.
// Hosts are compared case-insensitively with any port, IPv6 brackets, and
// trailing FQDN dot stripped.
//
// Reserved names (www, api, ...) are not special-cased — the Lookup decides:
// return ErrTenantNotFound for them and the chain continues. Panics with
// ErrEmptyName when base normalizes to "".
func Subdomain(base string) Source {
	base = normalizeHost(base)
	if base == "" {
		panic(ErrEmptyName)
	}
	return func(r *http.Request) (Identifier, bool) {
		host := normalizeHost(r.Host)
		rest, ok := strings.CutSuffix(host, base)
		if !ok || len(rest) < 2 || rest[len(rest)-1] != '.' {
			return Identifier{}, false
		}
		label := rest[:len(rest)-1]
		if strings.IndexByte(label, '.') >= 0 {
			return Identifier{}, false
		}
		return Identifier{Kind: KindSubdomain, Value: label}, true
	}
}

// Domain extracts the full (normalized) request host as KindDomain — the
// custom-domain path of a white-label SaaS. It extracts on every request
// with a host, so the Lookup's ErrTenantNotFound (which continues the chain)
// is what lets non-tenant hosts — the marketing site, the platform base
// domain — fall through to later sources or untenanted passthrough.
func Domain() Source {
	return func(r *http.Request) (Identifier, bool) {
		host := normalizeHost(r.Host)
		if host == "" {
			return Identifier{}, false
		}
		return Identifier{Kind: KindDomain, Value: host}, true
	}
}

// Header extracts a request header's value as KindID, e.g. "X-Tenant-ID".
//
// The value is attacker-controlled on any edge-facing listener: use this
// source only behind a trusted gateway that sets or strips the header, or
// for internal service-to-service traffic — the Lookup still gates which
// IDs are live. Panics with ErrEmptyName when name is empty.
func Header(name string) Source {
	if name == "" {
		panic(ErrEmptyName)
	}
	// Pre-canonicalize so Header.Get's own canonicalization hits its no-alloc
	// fast path on every request ("X-Tenant-ID" would otherwise allocate as
	// its canonical form is "X-Tenant-Id").
	name = textproto.CanonicalMIMEHeaderKey(name)
	return func(r *http.Request) (Identifier, bool) {
		v := r.Header.Get(name)
		return Identifier{Kind: KindID, Value: v}, v != ""
	}
}

// Cookie extracts a cookie's value as KindID. Like Header, the value is
// client-controlled — the Lookup gates which IDs are live; enforce
// membership downstream. Panics with ErrEmptyName when name is empty.
func Cookie(name string) Source {
	if name == "" {
		panic(ErrEmptyName)
	}
	return func(r *http.Request) (Identifier, bool) {
		c, err := r.Cookie(name)
		if err != nil || c.Value == "" { // only http.ErrNoCookie is possible
			return Identifier{}, false
		}
		return Identifier{Kind: KindID, Value: c.Value}, true
	}
}

// Query extracts a URL query parameter's value as KindID. Like Header, the
// value is client-controlled. Panics with ErrEmptyName when name is empty.
//
// It scans RawQuery directly instead of calling r.URL.Query(), which would
// allocate the full url.Values map per request (guard.Query precedent;
// benchmark in the PR). Semicolon-separated pairs are skipped, matching
// net/url's rejection of ";" in query strings.
//
// Unlike r.URL.Query(), the scan does not enforce net/url's 10,000-parameter
// cap, so it still extracts an identifier from a pathologically long query
// string (a benign divergence — the Lookup gates which tenants resolve, so
// extraction is not a bypass).
func Query(name string) Source {
	if name == "" {
		panic(ErrEmptyName)
	}
	return func(r *http.Request) (Identifier, bool) {
		q := r.URL.RawQuery
		for q != "" {
			var pair string
			pair, q, _ = strings.Cut(q, "&")
			if strings.Contains(pair, ";") {
				continue
			}
			k, v, _ := strings.Cut(pair, "=")
			if strings.ContainsAny(k, "%+") {
				dec, err := url.QueryUnescape(k)
				if err != nil {
					continue
				}
				k = dec
			}
			if k != name {
				continue
			}
			if strings.ContainsAny(v, "%+") {
				dec, err := url.QueryUnescape(v)
				if err != nil {
					continue
				}
				v = dec
			}
			return Identifier{Kind: KindID, Value: v}, v != ""
		}
		return Identifier{}, false
	}
}

// PathPrefix extracts the path segment following prefix as KindPath:
// PathPrefix("/t") yields "acme" from "/t/acme/dashboard", and
// PathPrefix("") yields the first segment. Whether the segment is a slug or
// the ID itself is the consumer's URL scheme — the Lookup interprets
// KindPath accordingly. The value is client-controlled; the Lookup gates
// which tenants are live. The path is never rewritten — stripping the
// prefix stays a routing concern. prefix must be empty or start with "/"
// and not end with "/"; otherwise the constructor panics with
// ErrInvalidPrefix.
func PathPrefix(prefix string) Source {
	if prefix != "" && (prefix[0] != '/' || prefix[len(prefix)-1] == '/') {
		panic(ErrInvalidPrefix)
	}
	return func(r *http.Request) (Identifier, bool) {
		rest, ok := strings.CutPrefix(r.URL.Path, prefix)
		if !ok || len(rest) < 2 || rest[0] != '/' {
			return Identifier{}, false
		}
		seg := rest[1:]
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		if seg == "" { // "/t//x": empty segment is not an identifier
			return Identifier{}, false
		}
		return Identifier{Kind: KindPath, Value: seg}, true
	}
}

// Context extracts a tenant already stamped on the request context by an
// upstream middleware — e.g. API-key authentication that called NewContext
// after verifying the key — as KindID. Middleware overrides any pre-existing
// context tenant, so this source is how a context-derived tenant gets an
// explicit slot in the precedence order; list it first so client-controlled
// sources cannot override an authenticated identity.
//
// The ID goes through the Lookup like any other KindID value — deliberately,
// so a tenant suspended mid-session stops resolving on the next request even
// though upstream auth already vouched for it. A consumer who wants to exempt
// upstream-verified IDs from that re-check can wire a custom source with its
// own Kind and fast-path it in their Lookup.
func Context() Source {
	return func(r *http.Request) (Identifier, bool) {
		id, ok := FromContext(r.Context())
		return Identifier{Kind: KindID, Value: id}, ok
	}
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
