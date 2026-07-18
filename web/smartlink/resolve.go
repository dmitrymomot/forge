package smartlink

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/resilience/cache"
)

// cachePrefix namespaces Manager's cache keys under a code, so a shared
// cache.Store can hold entries from other consumers without collision.
const cachePrefix = "smartlink:code:"

// Resolve returns the live Link stored under code: a cache read-through
// lookup (see [WithCache]) followed by liveness checks. A deactivated Link
// surfaces [ErrLinkDeactivated]; one whose ExpiresAt has passed surfaces
// [ErrLinkExpired]; an unknown code surfaces [ErrNotFound] from the Store.
//
// Resolve is the public, unscoped read path: a code is a public URL, so
// unlike the management ops it never consults [WithScope]. The returned
// Link has ShortURL populated (see [Manager.ShortURL]).
func (m *Manager) Resolve(ctx context.Context, code string) (Link, error) {
	l, err := m.lookup(ctx, code)
	if err != nil {
		return Link{}, err
	}
	if !l.DeactivatedAt.IsZero() {
		return Link{}, ErrLinkDeactivated
	}
	if !l.ExpiresAt.IsZero() && !l.ExpiresAt.After(time.Now()) {
		return Link{}, ErrLinkExpired
	}
	l.ShortURL = m.ShortURL(l.Code)
	return l, nil
}

// lookup reads code via cache read-through when [WithCache] is configured,
// else directly from the Store. A cache miss or any cache error falls
// through to the Store and best-effort repopulates the cache; cache errors
// are logged at debug and never fail the resolve — the Store stays the
// source of truth, so a bad cache backend degrades to "always hit the
// Store", not "links stop resolving".
func (m *Manager) lookup(ctx context.Context, code string) (Link, error) {
	if m.cfg.cacheStore == nil {
		return m.store.Get(ctx, code)
	}

	key := cachePrefix + code
	if l, ok := m.cacheGet(ctx, code, key); ok {
		return l, nil
	}

	l, err := m.store.Get(ctx, code)
	if err != nil {
		return Link{}, err
	}
	m.cacheSet(ctx, code, key, l)
	return l, nil
}

// cacheGet attempts the cache read-through hit path, reporting ok == false
// on a miss, a cache error, or a decode failure — any of which the caller
// treats identically: fall through to the Store.
func (m *Manager) cacheGet(ctx context.Context, code, key string) (Link, bool) {
	raw, err := m.cfg.cacheStore.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			m.cfg.logger.DebugContext(ctx, "smartlink: cache get failed", "code", code, "error", err)
		}
		return Link{}, false
	}
	var l Link
	if err := json.Unmarshal(raw, &l); err != nil {
		m.cfg.logger.DebugContext(ctx, "smartlink: cache decode failed", "code", code, "error", err)
		return Link{}, false
	}
	return l, true
}

// cacheSet best-effort writes l under key with the configured TTL. ShortURL
// is stripped before caching — it is derived from the base URL, never
// persisted, and must not leak through the cache either. Failures are
// logged at debug and otherwise ignored.
func (m *Manager) cacheSet(ctx context.Context, code, key string, l Link) {
	l.ShortURL = ""
	raw, err := json.Marshal(l)
	if err != nil {
		m.cfg.logger.DebugContext(ctx, "smartlink: cache encode failed", "code", code, "error", err)
		return
	}
	if err := m.cfg.cacheStore.Set(ctx, key, raw, cache.WithTTL(m.cfg.cacheTTL)); err != nil {
		m.cfg.logger.DebugContext(ctx, "smartlink: cache set failed", "code", code, "error", err)
	}
}

// invalidateCache best-effort evicts code's cache entry after a lifecycle
// mutation (Deactivate, Activate, Delete), bounding staleness of a warmed
// entry to at most the configured [WithCache] ttl (ttl is validated positive
// at construction, so an entry that survives a failed eviction here is
// always bounded). A no-op without [WithCache]; a failure is logged at
// debug, never surfaced — the mutation already succeeded against the Store.
func (m *Manager) invalidateCache(ctx context.Context, code string) {
	if m.cfg.cacheStore == nil {
		return
	}
	if err := m.cfg.cacheStore.Delete(ctx, cachePrefix+code); err != nil {
		m.cfg.logger.DebugContext(ctx, "smartlink: cache invalidate failed", "code", code, "error", err)
	}
}
