package tenant

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
)

// Validator checks that a resolved tenant ID identifies a tenant allowed to
// serve traffic. It is the status seam: implementations consult whatever
// system of record the consumer has (their tenants table, a cache, a control
// plane) and return nil for a live tenant, ErrTenantNotFound when the ID
// identifies no tenant, ErrTenantInactive when the tenant is soft-deleted,
// disabled, or suspended, and any other error for infrastructure failure.
// All non-nil errors fail resolution closed — a request with an invalid
// tenant is rejected, never passed through untenanted.
type Validator interface {
	ValidateTenant(ctx context.Context, id string) error
}

// ValidatorFunc adapts a plain function to the Validator interface.
type ValidatorFunc func(ctx context.Context, id string) error

// ValidateTenant calls f(ctx, id).
func (f ValidatorFunc) ValidateTenant(ctx context.Context, id string) error { return f(ctx, id) }

// ErrorHandler writes the HTTP response when Middleware fails to resolve a
// tenant for a reason other than "nothing resolved". err is the error
// returned by Resolve; match it with errors.Is against ErrTenantNotFound,
// ErrTenantInactive, and infrastructure errors from sources and validators.
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// Resolver derives, validates, and returns the canonical tenant ID for HTTP
// requests. Construct it with New; the zero value is not usable.
type Resolver struct {
	validate Validator
	onError  ErrorHandler
	log      *slog.Logger
	sources  []Source
}

// New builds a Resolver from options. At least one Source must be configured
// via WithSources or New panics with ErrNoSources. Sources run in the order
// given: the first one deriving a non-empty ID wins, and that ID is then
// checked by the WithValidator seam when one is configured.
func New(opts ...Option) *Resolver {
	c := newConfig(opts...)
	if len(c.sources) == 0 {
		panic(ErrNoSources)
	}
	return &Resolver{
		sources:  c.sources,
		validate: c.validate,
		onError:  c.onError,
		log:      c.log,
	}
}

// Resolve derives the tenant ID for r through the source chain and validates
// it. The first source deriving a non-empty ID wins; the chain does not
// continue past a validation failure — a resolved-but-invalid tenant fails
// closed with the validator's error (ErrTenantNotFound, ErrTenantInactive,
// or an infrastructure error). A source error also stops the chain. When no
// source derives anything, Resolve returns ErrNoTenant.
func (rv *Resolver) Resolve(r *http.Request) (string, error) {
	id, resolved, err := rv.resolve(r)
	if err == nil && !resolved {
		return "", ErrNoTenant
	}
	return id, err
}

// resolve reports "nothing resolved" through the resolved flag rather than a
// sentinel so Middleware's passthrough decision cannot be forged by a source
// or validator returning ErrNoTenant — any error, whatever its identity,
// fails closed.
func (rv *Resolver) resolve(r *http.Request) (string, bool, error) {
	for _, src := range rv.sources {
		id, err := src(r)
		if err != nil {
			return "", false, err
		}
		if id == "" {
			continue
		}
		if rv.validate != nil {
			if err := rv.validate.ValidateTenant(r.Context(), id); err != nil {
				return "", false, err
			}
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
// (default: 404 for ErrTenantNotFound/ErrTenantInactive, 500 otherwise) and
// next is not called.
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
				if errors.Is(err, ErrTenantNotFound) || errors.Is(err, ErrTenantInactive) {
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
	if errors.Is(err, ErrTenantNotFound) || errors.Is(err, ErrTenantInactive) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
