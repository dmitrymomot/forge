package shortlink

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// Alphabet is the code-generation charset: the standard base58 alphabet,
// which drops the ambiguous 0, O, I, and l so codes survive being read
// aloud or retyped.
const Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// generateAttempts bounds collision-retried code generation. At the default
// length 7 the space holds 58^7 ≈ 2.2e12 codes, so consecutive collisions
// signal a dangerously dense keyspace rather than bad luck.
const generateAttempts = 5

// maxVanityLength bounds requested vanity codes.
const maxVanityLength = 64

// cacheKeyPrefix namespaces resolved links in the shared cache store.
const cacheKeyPrefix = "shortlink:"

// defaultReserved is the built-in vanity blocklist: path words a SaaS app
// almost certainly routes itself. Extend with WithReservedCodes.
var defaultReserved = []string{
	"about", "account", "admin", "api", "app", "assets", "auth", "billing",
	"blog", "contact", "dashboard", "docs", "health", "healthz", "help",
	"home", "legal", "login", "logout", "me", "metrics", "new", "pricing",
	"privacy", "profile", "register", "root", "settings", "signin", "signup",
	"static", "status", "support", "terms", "www",
}

// Manager creates, manages, and resolves short links over a Store. Safe for
// concurrent use.
type Manager struct {
	store Store
	cache *cache.Cache[Link]
	cfg   config
}

// New builds a Manager. It panics on a nil store or invalid option values —
// wiring bugs caught at startup.
func New(store Store, opts ...Option) *Manager {
	if store == nil {
		panic("shortlink: nil store")
	}
	cfg := config{
		reserved:       make(map[string]struct{}, len(defaultReserved)),
		schemes:        map[string]struct{}{"http": {}, "https": {}},
		cacheTTL:       5 * time.Minute,
		codeLength:     7,
		redirectStatus: http.StatusFound,
	}
	for _, w := range defaultReserved {
		cfg.reserved[w] = struct{}{}
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.codeLength < 4 || cfg.codeLength > 32 {
		panic(fmt.Sprintf("shortlink: code length %d outside [4, 32]", cfg.codeLength))
	}
	if cfg.cacheTTL <= 0 {
		panic("shortlink: cache TTL must be positive")
	}
	if len(cfg.schemes) == 0 {
		panic("shortlink: empty scheme allowlist")
	}
	if cfg.redirectStatus != http.StatusFound && cfg.redirectStatus != http.StatusTemporaryRedirect {
		panic(fmt.Sprintf("shortlink: redirect status %d is not 302 or 307", cfg.redirectStatus))
	}
	m := &Manager{store: store, cfg: cfg}
	if cfg.cache != nil {
		m.cache = cache.New[Link](cfg.cache,
			cache.WithPrefix(cacheKeyPrefix), cache.WithDefaultTTL(cfg.cacheTTL))
	}
	return m
}

// Create stores a link to p.URL and returns the record. With p.Code empty a
// code is generated from Alphabet, retrying on collision; a requested
// vanity code is validated against the charset, length, and reserved
// blocklist and fails with ErrDuplicate when taken.
func (m *Manager) Create(ctx context.Context, p CreateParams) (Link, error) {
	if err := m.validateURL(p.URL); err != nil {
		return Link{}, err
	}
	tenant, err := m.scoped(ctx, p.Tenant)
	if err != nil {
		return Link{}, err
	}
	l := Link{
		URL:       p.URL,
		Tenant:    tenant,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: p.ExpiresAt,
	}
	if p.Code != "" {
		if err := m.validateVanity(p.Code); err != nil {
			return Link{}, err
		}
		l.Code = p.Code
		if err := m.store.Create(ctx, l); err != nil {
			return Link{}, fmt.Errorf("shortlink: create: %w", err)
		}
		return l, nil
	}
	for range generateAttempts {
		l.Code = random.String(m.cfg.codeLength, Alphabet)
		if _, reserved := m.cfg.reserved[strings.ToLower(l.Code)]; reserved {
			continue
		}
		err := m.store.Create(ctx, l)
		if errors.Is(err, ErrDuplicate) {
			continue
		}
		if err != nil {
			return Link{}, fmt.Errorf("shortlink: create: %w", err)
		}
		return l, nil
	}
	return Link{}, ErrCodeExhausted
}

// Get returns one link record straight from the Store. With WithScope
// configured, other tenants' links read as ErrNotFound so existence cannot
// be probed across tenants.
func (m *Manager) Get(ctx context.Context, code string) (Link, error) {
	tenant, err := m.scoped(ctx, "")
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
	return l, nil
}

// List returns links matching f, newest first. With WithScope configured
// the filter is confined to the scoped tenant.
func (m *Manager) List(ctx context.Context, f Filter) ([]Link, error) {
	tenant, err := m.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, err
	}
	f.Tenant = tenant
	return m.store.List(ctx, f)
}

// Deactivate turns the link off: Resolve reports ErrLinkDeactivated until
// Activate. Under WithScope, other tenants' links report ErrNotFound.
func (m *Manager) Deactivate(ctx context.Context, code string) error {
	at := time.Now().UTC()
	return m.mutate(ctx, code, "deactivate", func(tenant string) error {
		return m.store.Deactivate(ctx, code, tenant, at)
	})
}

// Activate turns a deactivated link back on.
func (m *Manager) Activate(ctx context.Context, code string) error {
	return m.mutate(ctx, code, "activate", func(tenant string) error {
		return m.store.Activate(ctx, code, tenant)
	})
}

// Delete permanently removes the link. The code becomes available again.
func (m *Manager) Delete(ctx context.Context, code string) error {
	return m.mutate(ctx, code, "delete", func(tenant string) error {
		return m.store.Delete(ctx, code, tenant)
	})
}

// mutate runs one scope-confined store mutation and invalidates the cached
// entry. The tenant predicate rides into the store call so confinement is
// atomic with the mutation — a pre-flight ownership check would race a
// delete-and-recreate of the same code by another tenant. The cache entry
// is dropped even when the store reports ErrNotFound: the record may be
// gone from the store while a stale copy lingers in the cache (e.g. a
// crashed earlier invalidation), and this is the only API path that can
// clear it.
func (m *Manager) mutate(ctx context.Context, code, verb string, op func(tenant string) error) error {
	tenant, err := m.scoped(ctx, "")
	if err != nil {
		return err
	}
	opErr := op(tenant)
	if opErr != nil && !errors.Is(opErr, ErrNotFound) {
		return fmt.Errorf("shortlink: %s: %w", verb, opErr)
	}
	m.invalidate(ctx, code)
	return opErr
}

// Resolve returns the live link for code — the redirect hot path. It is
// unscoped (a short code is a public URL) and read-through cached when
// WithCache is configured. Unknown codes report ErrNotFound, expired links
// ErrLinkExpired, deactivated links ErrLinkDeactivated. On success the
// WithOnHit hook fires synchronously before Resolve returns.
func (m *Manager) Resolve(ctx context.Context, code string) (Link, error) {
	if code == "" {
		return Link{}, ErrNotFound
	}
	l, err := m.lookup(ctx, code)
	if err != nil {
		return Link{}, err
	}
	if !l.DeactivatedAt.IsZero() {
		return Link{}, ErrLinkDeactivated
	}
	if !l.ExpiresAt.IsZero() && !l.ExpiresAt.After(time.Now().UTC()) {
		return Link{}, ErrLinkExpired
	}
	if m.cfg.onHit != nil {
		m.cfg.onHit(ctx, l)
	}
	return l, nil
}

// lookup fetches the raw record, serving from cache when configured.
// Expiry and deactivation are re-checked by Resolve on every hit, so
// caching the raw record is safe. Cache failures degrade to the store, and
// cache writes are best-effort — the cache can only slow resolution down,
// never break it.
func (m *Manager) lookup(ctx context.Context, code string) (Link, error) {
	if m.cache == nil {
		return m.store.Get(ctx, code)
	}
	if l, err := m.cache.Get(ctx, code); err == nil {
		return l, nil
	}
	l, err := m.store.Get(ctx, code)
	if err != nil {
		return Link{}, err
	}
	_ = m.cache.Set(ctx, code, l)
	return l, nil
}

// invalidate drops the cached entry after a mutation, best-effort. Cache
// coherence is bounded, not exact: an in-flight lookup that read the store
// before the mutation can re-populate the entry right after this delete,
// and a failed delete leaves the stale entry in place — either way the
// entry dies within the WithCacheTTL bound, which is the staleness
// contract WithCache documents. Failing the mutation over it would claim a
// precision the read path cannot honor.
func (m *Manager) invalidate(ctx context.Context, code string) {
	if m.cache != nil {
		_ = m.cache.Delete(ctx, code)
	}
}

// scoped resolves the tenant a management operation is confined to. With
// no WithScope hook it passes the requested tenant through. Fail-closed:
// hook errors and empty scoped tenants abort the operation with ErrScope;
// an explicit requested tenant must be empty or equal to the scoped one.
func (m *Manager) scoped(ctx context.Context, requested string) (string, error) {
	if m.cfg.scope == nil {
		return requested, nil
	}
	t, err := m.cfg.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if t == "" {
		return "", ErrScope
	}
	if requested != "" && requested != t {
		return "", ErrTenantMismatch
	}
	return t, nil
}

// validateURL enforces the server-side-destination rules: absolute, with a
// host, scheme on the allowlist.
func (m *Manager) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if !u.IsAbs() || u.Host == "" {
		return ErrInvalidURL
	}
	if _, ok := m.cfg.schemes[strings.ToLower(u.Scheme)]; !ok {
		return fmt.Errorf("%w: %q", ErrSchemeNotAllowed, u.Scheme)
	}
	return nil
}

// validateVanity enforces vanity-code charset ([A-Za-z0-9_-]), length
// (1–64), and the reserved blocklist (case-insensitive).
func (m *Manager) validateVanity(code string) error {
	if len(code) > maxVanityLength {
		return fmt.Errorf("%w: longer than %d characters", ErrInvalidCode, maxVanityLength)
	}
	for i := range len(code) {
		c := code[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return fmt.Errorf("%w: %q", ErrInvalidCode, code)
		}
	}
	if _, ok := m.cfg.reserved[strings.ToLower(code)]; ok {
		return fmt.Errorf("%w: %q", ErrReservedCode, code)
	}
	return nil
}
