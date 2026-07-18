package smartlink

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync/atomic"
	"time"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/singleflight"
)

// maxCodeAttempts caps how many times Create retries a colliding generated
// code before giving up.
const maxCodeAttempts = 5

// Manager is the management surface over a [Store]: it validates and
// creates Links (vanity or generated code, Target or Ref, tenant scope) and
// drives their lifecycle (Get, List, Deactivate, Activate, Delete).
// Resolving a code to a live Link is [Manager.Resolve]; serving it as a
// redirect is [Manager.Handler]. Safe for concurrent use — all state lives
// in the Store.
type Manager struct {
	store Store
	links *cache.Cache[Link] // typed facade over WithCache's store; nil without WithCache
	// decorate is the precomputed WithDecorators chain the Handler wraps
	// around every link's Decider; nil when none are configured.
	decorate Decorator
	// flight dedups concurrent cache-miss lookups per code (DoDetached, so a
	// stuck Store read never pins a request past its own deadline), and
	// cacheGen guards their write-backs against racing lifecycle mutations
	// (see lookup and invalidateCache).
	flight   singleflight.Group[Link]
	cfg      managerConfig
	cacheGen atomic.Uint64
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
	m := &Manager{store: store, cfg: *cfg}
	if len(cfg.decorators) > 0 {
		m.decorate = Chain(cfg.decorators...)
	}
	if cfg.cacheStore != nil {
		m.links = cache.New[Link](cfg.cacheStore, cache.WithPrefix(cachePrefix), cache.WithDefaultTTL(cfg.cacheTTL))
	}
	return m, nil
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
		if _, err := m.compileLink("", p.Target); err != nil {
			return Link{}, err
		}
	}
	if p.Ref != "" && !p.SkipRefCheck && m.cfg.resolver != nil {
		d, err := m.cfg.resolver(ctx, Link{Ref: p.Ref, Tenant: p.Tenant})
		if err != nil {
			// Only a ref the resolver positively rejects is caller input error;
			// anything else (consumer DB down, cache backend failing) is an
			// infrastructure failure and must not read as ErrInvalidLink to an
			// API layer mapping it to a 4xx.
			if errors.Is(err, ErrRefNotFound) || errors.Is(err, ErrNoTarget) {
				return Link{}, fmt.Errorf("%w: ref %q: %w", ErrInvalidLink, p.Ref, err)
			}
			return Link{}, fmt.Errorf("smartlink: check ref %q: %w", p.Ref, err)
		}
		// A (nil, nil) resolver is a consumer bug, not caller input: reject it
		// here rather than letting the link store successfully and panic (now
		// 500) on its first hit.
		if d == nil {
			return Link{}, fmt.Errorf("smartlink: check ref %q: resolver returned nil Decider without error", p.Ref)
		}
	}

	l := Link{
		ExpiresAt: p.ExpiresAt,
		Target:    p.Target,
		Ref:       p.Ref,
		Code:      p.Code,
		Metadata:  maps.Clone(p.Metadata),
		Tenant:    p.Tenant,
		// Truncated to microseconds — Postgres timestamptz precision — so the
		// returned Link carries the same instant a later Get reads back and
		// List ordering agrees across Store implementations.
		CreatedAt: m.cfg.clock.Now().UTC().Truncate(time.Microsecond),
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
// creates it, retrying on ErrDuplicate up to maxCodeAttempts times. Generated
// codes face the same charset and reserved-word rules as vanity codes — a
// short random generator can legitimately emit a reserved word like "api"
// (retried like a collision), while a charset/length violation means the
// generator itself is broken and fails immediately rather than looping. An
// empty string is treated as a failed attempt (not stored) and retried, so a
// misbehaving generator exhausts the loop and surfaces the same error as an
// unlucky collision streak instead of silently persisting an empty code.
func (m *Manager) createGenerated(ctx context.Context, l *Link) error {
	lastErr := errors.New("generated code was empty")
	for range maxCodeAttempts {
		l.Code = m.cfg.codeFunc()
		if l.Code == "" {
			continue
		}
		if !validCodeChars(l.Code) {
			return fmt.Errorf("%w: generated code %q must be 1-64 characters of [A-Za-z0-9_-]", ErrInvalidLink, l.Code)
		}
		if _, reserved := m.cfg.reserved[l.Code]; reserved {
			lastErr = fmt.Errorf("%w: code %q", ErrCodeReserved, l.Code)
			continue
		}
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

// compileLink compiles target as the sole default target of a degenerate
// Spec under the configured link param policy — surfacing template errors
// (bad macro, malformed template) as ErrInvalidLink — then applies the URL
// policy checks via checkTargetURL. It is the single definition of what a
// valid Target-backed Link is: Create validates through it (salt irrelevant,
// result discarded) and [Manager.decider] serves through it (salted by the
// link code so split/Percent bucketing stays per-link), so a row written to
// the Store outside this Manager faces the same scheme policy at serve time
// (defense in depth — a directly-inserted "javascript:" target must not
// become a redirect).
func (m *Manager) compileLink(salt, target string) (*Compiled, error) {
	compiled, err := Compile(Spec{Salt: salt, Default: []Target{{URL: target}}, Params: m.cfg.linkParamPolicy})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidLink, err)
	}
	if err := m.checkTargetURL(&compiled.def.targets[0].tmpl); err != nil {
		return nil, err
	}
	return compiled, nil
}

// checkTargetURL enforces the URL policy on a compiled template, reading the
// parse Compile already did: the macro-elided scheme must be on the
// allowlist and non-empty, and the host non-empty unless the authority is
// itself (at least partly) a macro, in which case a dynamic-host template
// stays legal. Reached through [Manager.compileLink] from both Create
// validation and the per-hit serve path.
func (m *Manager) checkTargetURL(t *template) error {
	u := t.elidedURL
	if u.Scheme == "" || !slices.Contains(m.cfg.schemes, u.Scheme) {
		return fmt.Errorf("%w: scheme %q not allowed", ErrInvalidLink, u.Scheme)
	}
	if u.Host == "" && !t.authMacro {
		return fmt.Errorf("%w: target %q has no host", ErrInvalidLink, t.raw)
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
	return m.mutateLink(ctx, code, func(ctx context.Context, tenant string) error {
		return m.store.Deactivate(ctx, code, tenant, m.cfg.clock.Now().UTC())
	})
}

// Activate clears a prior Deactivate. See [Manager.Deactivate] for scope
// handling and cache invalidation.
func (m *Manager) Activate(ctx context.Context, code string) error {
	return m.mutateLink(ctx, code, func(ctx context.Context, tenant string) error {
		return m.store.Activate(ctx, code, tenant)
	})
}

// Delete removes code. See [Manager.Deactivate] for scope handling and
// cache invalidation.
func (m *Manager) Delete(ctx context.Context, code string) error {
	return m.mutateLink(ctx, code, func(ctx context.Context, tenant string) error {
		return m.store.Delete(ctx, code, tenant)
	})
}

// mutateLink is the shared lifecycle-mutation sequence — resolve the tenant
// scope, run the store op with it, evict the cache entry on success — so the
// tenant-safety and cache-consistency invariant is stated once, not per
// method.
func (m *Manager) mutateLink(ctx context.Context, code string, op func(ctx context.Context, tenant string) error) error {
	tenant, err := m.tenantScope(ctx)
	if err != nil {
		return err
	}
	if err := op(ctx, tenant); err != nil {
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
