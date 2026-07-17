package smartlink

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"time"
)

// maxCodeAttempts caps how many times Create retries a colliding generated
// code before giving up.
const maxCodeAttempts = 5

// Manager is the management surface over a [Store]: it validates and
// creates Links (vanity or generated code, Target or Ref, tenant scope) and
// drives their lifecycle (Get, List, Deactivate, Activate, Delete).
// Resolving a code to a redirect decision is a Handler's job (a later
// task). Safe for concurrent use — all state lives in the Store.
type Manager struct {
	store Store
	cfg   managerConfig
}

// NewManager builds a Manager over store. It returns an error if any
// ManagerOption failed validation (e.g. an invalid WithBaseURL or an
// unsupported WithRedirectStatus code).
func NewManager(store Store, opts ...ManagerOption) (*Manager, error) {
	if store == nil {
		return nil, errors.New("smartlink: nil store")
	}
	cfg := newManagerConfig()
	for _, o := range opts {
		o(cfg)
	}
	if len(cfg.errs) > 0 {
		return nil, errors.Join(cfg.errs...)
	}
	return &Manager{store: store, cfg: *cfg}, nil
}

// Create validates p and inserts a new Link. Exactly one of Target or Ref
// must be set, else [ErrInvalidLink]. A caller-supplied Code (vanity) must
// be 1-64 characters of [A-Za-z0-9_-] and not in the reserved blocklist
// (else [ErrInvalidLink]/[ErrCodeReserved]); a collision surfaces
// [ErrDuplicate] from the Store. An empty Code is generated via the
// configured code function, retried up to 5 times on a collision before
// giving up with a wrapped error. Metadata is cloned. The returned Link has
// ShortURL populated (see [Manager.ShortURL]); the record handed to the
// Store never carries one, since it is derived, not persisted.
func (m *Manager) Create(ctx context.Context, p CreateParams) (Link, error) {
	if (p.Target == "") == (p.Ref == "") {
		return Link{}, fmt.Errorf("%w: exactly one of Target or Ref is required", ErrInvalidLink)
	}

	tenant, err := m.tenantScope(ctx)
	if err != nil {
		return Link{}, err
	}
	if m.cfg.scope != nil {
		switch {
		case p.Tenant == "":
			p.Tenant = tenant
		case p.Tenant != tenant:
			return Link{}, ErrScope
		}
	}

	if p.Code != "" {
		if err := m.validateVanityCode(p.Code); err != nil {
			return Link{}, err
		}
	}
	if p.Target != "" {
		if err := m.validateTarget(p.Target); err != nil {
			return Link{}, err
		}
	}
	if p.Ref != "" && !p.SkipRefCheck && m.cfg.resolver != nil {
		if _, err := m.cfg.resolver(ctx, Link{Ref: p.Ref, Tenant: p.Tenant}); err != nil {
			return Link{}, fmt.Errorf("%w: ref %q: %w", ErrInvalidLink, p.Ref, err)
		}
	}

	l := Link{
		ExpiresAt: p.ExpiresAt,
		Target:    p.Target,
		Ref:       p.Ref,
		Code:      p.Code,
		Metadata:  maps.Clone(p.Metadata),
		Tenant:    p.Tenant,
		CreatedAt: time.Now().UTC(),
	}

	if l.Code != "" {
		if err := m.store.Create(ctx, l); err != nil {
			return Link{}, err
		}
	} else if err := m.createGenerated(ctx, &l); err != nil {
		return Link{}, err
	}

	l.ShortURL = m.ShortURL(l.Code)
	return l, nil
}

// createGenerated assigns l.Code from the configured code function and
// creates it, retrying on ErrDuplicate up to maxCodeAttempts times.
func (m *Manager) createGenerated(ctx context.Context, l *Link) error {
	var lastErr error
	for range maxCodeAttempts {
		l.Code = m.cfg.codeFunc()
		err := m.store.Create(ctx, *l)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrDuplicate) {
			return err
		}
		lastErr = err
	}
	m.cfg.logger.WarnContext(ctx, "smartlink: exhausted code generation attempts", "attempts", maxCodeAttempts)
	return fmt.Errorf("smartlink: generate unique code after %d attempts: %w", maxCodeAttempts, lastErr)
}

// validateVanityCode checks a caller-supplied Code against the charset,
// length, and reserved-word rules. The charset check runs first, so a
// reserved word containing a dot ("favicon.ico", "robots.txt",
// ".well-known") is rejected as ErrInvalidLink rather than
// ErrCodeReserved — it can never be a valid vanity code regardless of the
// blocklist.
func (m *Manager) validateVanityCode(code string) error {
	if !validCodeChars(code) {
		return fmt.Errorf("%w: code %q must be 1-64 characters of [A-Za-z0-9_-]", ErrInvalidLink, code)
	}
	if _, reserved := m.cfg.reserved[code]; reserved {
		return fmt.Errorf("%w: code %q", ErrCodeReserved, code)
	}
	return nil
}

// validateTarget compiles raw as the sole default target of a degenerate
// Spec — surfacing template errors (bad macro, malformed template) as
// ErrInvalidLink — then checks its macro-elided scheme is on the allowlist
// and non-empty, and that its host is non-empty unless the authority is
// itself (at least partly) a macro, in which case a dynamic-host template
// stays legal.
func (m *Manager) validateTarget(raw string) error {
	if _, err := Compile(Spec{Default: []Target{{URL: raw}}, Params: m.cfg.linkParamPolicy}); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidLink, err)
	}
	u, err := url.Parse(macroElide(raw))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidLink, err)
	}
	if u.Scheme == "" || !slices.Contains(m.cfg.schemes, u.Scheme) {
		return fmt.Errorf("%w: scheme %q not allowed", ErrInvalidLink, u.Scheme)
	}
	if u.Host == "" && !authorityHasMacro(raw) {
		return fmt.Errorf("%w: target %q has no host", ErrInvalidLink, raw)
	}
	return nil
}

// Get returns the Link stored under code, with ShortURL populated. With
// WithScope configured, a record belonging to a different tenant reads as
// [ErrNotFound] so existence cannot be probed across tenants.
func (m *Manager) Get(ctx context.Context, code string) (Link, error) {
	tenant, err := m.tenantScope(ctx)
	if err != nil {
		return Link{}, err
	}
	l, err := m.store.Get(ctx, code)
	if err != nil {
		return Link{}, err
	}
	if m.cfg.scope != nil && l.Tenant != tenant {
		return Link{}, ErrNotFound
	}
	l.ShortURL = m.ShortURL(l.Code)
	return l, nil
}

// List returns Links matching f, with ShortURL populated on each. With
// WithScope configured, f.Tenant is forced to the scoped tenant regardless
// of the caller-supplied value.
func (m *Manager) List(ctx context.Context, f Filter) ([]Link, error) {
	tenant, err := m.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	if m.cfg.scope != nil {
		f.Tenant = tenant
	}
	links, err := m.store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	for i := range links {
		links[i].ShortURL = m.ShortURL(links[i].Code)
	}
	return links, nil
}

// Deactivate marks code inactive. With WithScope configured, the scoped
// tenant is passed as the Store predicate, so a foreign-tenant code is
// untouched and reads back as [ErrNotFound]. On success it best-effort
// evicts any cached entry for code (see [Manager.invalidateCache]).
func (m *Manager) Deactivate(ctx context.Context, code string) error {
	tenant, err := m.tenantScope(ctx)
	if err != nil {
		return err
	}
	if err := m.store.Deactivate(ctx, code, tenant, time.Now().UTC()); err != nil {
		return err
	}
	m.invalidateCache(ctx, code)
	return nil
}

// Activate clears a prior Deactivate. See [Manager.Deactivate] for scope
// handling and cache invalidation.
func (m *Manager) Activate(ctx context.Context, code string) error {
	tenant, err := m.tenantScope(ctx)
	if err != nil {
		return err
	}
	if err := m.store.Activate(ctx, code, tenant); err != nil {
		return err
	}
	m.invalidateCache(ctx, code)
	return nil
}

// Delete removes code. See [Manager.Deactivate] for scope handling and
// cache invalidation.
func (m *Manager) Delete(ctx context.Context, code string) error {
	tenant, err := m.tenantScope(ctx)
	if err != nil {
		return err
	}
	if err := m.store.Delete(ctx, code, tenant); err != nil {
		return err
	}
	m.invalidateCache(ctx, code)
	return nil
}

// ShortURL renders code onto the base URL from [WithBaseURL]. Without that
// option it returns "".
func (m *Manager) ShortURL(code string) string {
	if m.cfg.baseURL == "" {
		return ""
	}
	return m.cfg.baseURL + code
}

// tenantScope resolves the management-op tenant from the configured scope
// hook, failing closed with ErrScope on a hook error or an empty result.
// Without WithScope it returns "" (unscoped, verbatim passthrough).
func (m *Manager) tenantScope(ctx context.Context) (string, error) {
	if m.cfg.scope == nil {
		return "", nil
	}
	t, err := m.cfg.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if t == "" {
		return "", ErrScope
	}
	return t, nil
}
