package shortlink

import (
	"context"
	"encoding/json"
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
	return &Manager{store: store, cfg: cfg}
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
// Activate. The cached entry is invalidated; an invalidation error is
// returned even though the store was updated — retry, the call is
// idempotent.
func (m *Manager) Deactivate(ctx context.Context, code string) error {
	if _, err := m.Get(ctx, code); err != nil {
		return err
	}
	if err := m.store.Deactivate(ctx, code, time.Now().UTC()); err != nil {
		return fmt.Errorf("shortlink: deactivate: %w", err)
	}
	return m.invalidate(ctx, code)
}

// Activate turns a deactivated link back on and invalidates the cached
// entry.
func (m *Manager) Activate(ctx context.Context, code string) error {
	if _, err := m.Get(ctx, code); err != nil {
		return err
	}
	if err := m.store.Activate(ctx, code); err != nil {
		return fmt.Errorf("shortlink: activate: %w", err)
	}
	return m.invalidate(ctx, code)
}

// Delete permanently removes the link and invalidates the cached entry.
// The code becomes available again.
func (m *Manager) Delete(ctx context.Context, code string) error {
	if _, err := m.Get(ctx, code); err != nil {
		return err
	}
	if err := m.store.Delete(ctx, code); err != nil {
		return fmt.Errorf("shortlink: delete: %w", err)
	}
	return m.invalidate(ctx, code)
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
// caching the raw record is safe; mutations invalidate the entry. Cache
// failures degrade to the store, and cache writes are best-effort — the
// cache can only slow resolution down, never break it.
func (m *Manager) lookup(ctx context.Context, code string) (Link, error) {
	if m.cfg.cache == nil {
		return m.store.Get(ctx, code)
	}
	key := cacheKeyPrefix + code
	if b, err := m.cfg.cache.Get(ctx, key); err == nil {
		var l Link
		if err := json.Unmarshal(b, &l); err == nil {
			return l, nil
		}
	}
	l, err := m.store.Get(ctx, code)
	if err != nil {
		return Link{}, err
	}
	if b, err := json.Marshal(l); err == nil {
		_ = m.cfg.cache.Set(ctx, key, b, cache.WithTTL(m.cfg.cacheTTL))
	}
	return l, nil
}

// invalidate drops the cached entry after a mutation. A failure is
// surfaced: silently keeping a stale entry would serve a deactivated or
// deleted link until the TTL runs out.
func (m *Manager) invalidate(ctx context.Context, code string) error {
	if m.cfg.cache == nil {
		return nil
	}
	if err := m.cfg.cache.Delete(ctx, cacheKeyPrefix+code); err != nil {
		return fmt.Errorf("shortlink: cache invalidate: %w", err)
	}
	return nil
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
