package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
)

// Lookup is the single consumer seam: it translates an extracted Identifier
// into a live canonical tenant ID, answering "which tenant" and "may it
// serve traffic" in one call (typically one query). Implementations switch
// on ident.Kind and return:
//
//   - the canonical tenant ID for a live tenant;
//   - ErrTenantNotFound when the identifier maps to no live tenant — the
//     Resolver continues with the next source (a request may legitimately
//     carry a non-tenant host, e.g. the marketing site);
//   - ErrTenantInactive when the tenant exists but must not serve traffic
//     (soft-deleted, disabled, suspended) — resolution fails closed;
//   - any other error for infrastructure failure — resolution fails closed.
//
// Implementations must be safe for concurrent use.
type Lookup interface {
	LookupTenant(ctx context.Context, ident Identifier) (string, error)
}

// LookupFunc adapts a plain function to the Lookup interface.
type LookupFunc func(ctx context.Context, ident Identifier) (string, error)

// LookupTenant implements Lookup.
func (f LookupFunc) LookupTenant(ctx context.Context, ident Identifier) (string, error) {
	return f(ctx, ident)
}

// ErrorHandler writes the HTTP response when Middleware fails to resolve a
// tenant for a reason other than "nothing resolved". err is the error
// returned by Resolve; match it with errors.Is against ErrTenantInactive
// and infrastructure errors from the Lookup.
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// errEmptyID guards against a Lookup returning ("", nil) — a consumer bug;
// resolution fails closed rather than admitting an empty tenant.
var errEmptyID = errors.New("tenant: lookup returned an empty tenant id")

// Resolver derives, validates, and returns the canonical tenant ID for HTTP
// requests. Construct it with New; the zero value is not usable.
type Resolver struct {
	lookup  Lookup
	onError ErrorHandler
	log     *slog.Logger
	sources []Source
}

// New builds a Resolver over the given Lookup. At least one Source must be
// configured via WithSources or New panics with ErrNoSources; a nil lookup
// panics with ErrNilLookup — both are wiring bugs. Sources run in the order
// given: the first one extracting an identifier that the Lookup resolves to
// a live tenant wins.
func New(lookup Lookup, opts ...Option) *Resolver {
	if lookup == nil {
		panic(ErrNilLookup)
	}
	c := newConfig(opts...)
	if len(c.sources) == 0 {
		panic(ErrNoSources)
	}
	return &Resolver{
		lookup:  lookup,
		onError: c.onError,
		log:     c.log,
		sources: c.sources,
	}
}

// Resolve derives the tenant ID for r: each source in turn extracts a
// candidate identifier, and the Lookup translates it to a live canonical
// ID. ErrTenantNotFound from the Lookup means "this identifier belongs to
// no live tenant" and the chain continues with the next source; any other
// Lookup error fails closed and stops the chain. When no source yields a
// live tenant, Resolve returns ErrNoTenant.
func (rv *Resolver) Resolve(r *http.Request) (string, error) {
	id, resolved, err := rv.resolve(r)
	if err == nil && !resolved {
		return "", ErrNoTenant
	}
	return id, err
}

// resolve reports "nothing resolved" through the resolved flag rather than a
// sentinel so Middleware's passthrough decision cannot be forged by a Lookup
// returning ErrNoTenant — any error, whatever its identity, fails closed.
func (rv *Resolver) resolve(r *http.Request) (string, bool, error) {
	for _, src := range rv.sources {
		ident, ok := src(r)
		if !ok || ident.Value == "" {
			continue
		}
		id, err := rv.lookup.LookupTenant(r.Context(), ident)
		if err != nil {
			if errors.Is(err, ErrTenantNotFound) {
				continue
			}
			return "", false, err
		}
		if id == "" {
			return "", false, fmt.Errorf("%w (%s %q)", errEmptyID, ident.Kind, ident.Value)
		}
		return id, true, nil
	}
	return "", false, nil
}

// Middleware resolves the tenant for each request and stamps it on the
// request context via NewContext, overriding any tenant already there (give
// a pre-existing context tenant an explicit slot with the Context source).
// When nothing resolves, the request passes through unchanged: a tenant
// stamped by an upstream middleware is preserved, and a request with none
// stays untenanted so single-tenant routes coexist — add Require where
// tenancy is mandatory. Any failure is delegated to the error handler
// (default: 404 for ErrTenantInactive, 500 otherwise) and next is not
// called.
//
// Because resolution overrides upstream identity, any client-controlled
// source (Header, Cookie, Query, PathPrefix) placed before Context() lets a
// client swap an authenticated tenant for a different live one — order
// Context() first when an upstream middleware stamps a verified tenant, and
// enforce membership downstream regardless.
func (rv *Resolver) Middleware() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, resolved, err := rv.resolve(r)
			switch {
			case err == nil && resolved:
				next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), id)))
			case err == nil:
				next.ServeHTTP(w, r)
			default:
				level := slog.LevelError
				if errors.Is(err, ErrTenantInactive) {
					level = slog.LevelDebug
				}
				if rv.log.Enabled(r.Context(), level) {
					rv.log.LogAttrs(r.Context(), level, "tenant: resolution failed",
						slog.Any("error", err), slog.String("host", r.Host))
				}
				rv.onError(w, r, err)
			}
		})
	}
}

// Require is a guard middleware responding 404 Not Found when the request
// context carries no tenant. 404 — not 401/403 — because an unresolved
// tenant host has nothing there, and the status leaks nothing about
// tenancy. Place it after Middleware on routes where tenancy is mandatory:
//
//	middleware.Chain(resolver.Middleware(), tenant.Require)
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// defaultErrorHandler maps rejection errors to 404 (leaking nothing about
// why the tenant is unavailable) and everything else to a generic 500.
func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrTenantInactive) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
