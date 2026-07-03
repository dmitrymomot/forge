package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/resilience/singleflight"
)

type config struct {
	marshaler  any // Marshaler[V] set by WithMarshaler; resolved in New
	prefix     string
	defaultTTL time.Duration
}

// Option configures a Cache. Options are non-generic so call sites need no type
// arguments; WithMarshaler infers V from its argument.
type Option func(*config)

// WithPrefix namespaces this cache's keys within its Store, isolating it from
// other caches sharing the same Store.
func WithPrefix(p string) Option { return func(c *config) { c.prefix = p } }

// WithDefaultTTL is applied when Set receives ttl == 0 (default 5m).
func WithDefaultTTL(d time.Duration) Option { return func(c *config) { c.defaultTTL = d } }

// WithMarshaler overrides the default JSON serialization. Its type parameter
// must match the value type of the Cache it is passed to: New[V] panics if the
// marshaler is a Marshaler[W] for some W != V (the Option type is non-generic,
// so the mismatch cannot be caught at compile time).
func WithMarshaler[V any](m Marshaler[V]) Option {
	return func(c *config) {
		if m != nil {
			c.marshaler = m
		}
	}
}

// Cache is a typed facade over a Store. It marshals values, applies the default
// TTL, isolates keys by prefix, and provides GetOrSet. It does NOT own the
// Store: construct and Close the Store yourself.
type Cache[V any] struct {
	store      Store
	marshaler  Marshaler[V]
	prefix     string
	sf         singleflight.Group[V]
	defaultTTL time.Duration
}

// New builds a typed cache over store. The Store's lifecycle stays with the
// caller; Cache has no Close.
func New[V any](store Store, opts ...Option) *Cache[V] {
	c := config{defaultTTL: 5 * time.Minute}
	for _, o := range opts {
		o(&c)
	}
	var m Marshaler[V]
	if c.marshaler != nil {
		tm, ok := c.marshaler.(Marshaler[V])
		if !ok {
			panic(fmt.Sprintf("cache: WithMarshaler marshaler %T does not match the Cache value type", c.marshaler))
		}
		m = tm
	} else {
		m = jsonMarshaler[V]{}
	}
	return &Cache[V]{store: store, prefix: c.prefix, defaultTTL: c.defaultTTL, marshaler: m}
}

func (c *Cache[V]) key(k string) string { return c.prefix + k }

// Get returns the value for key or ErrNotFound.
func (c *Cache[V]) Get(ctx context.Context, key string) (V, error) {
	var zero V
	data, err := c.store.Get(ctx, c.key(key))
	if err != nil {
		return zero, err
	}
	return c.marshaler.Unmarshal(data)
}

// Set stores v under key. ttl == 0 uses the configured default; ttl < 0 never
// expires.
func (c *Cache[V]) Set(ctx context.Context, key string, v V, ttl time.Duration) error {
	data, err := c.marshaler.Marshal(v)
	if err != nil {
		return err
	}
	if ttl == 0 {
		ttl = c.defaultTTL
	}
	return c.store.Set(ctx, c.key(key), data, ttl)
}

// Delete removes key.
func (c *Cache[V]) Delete(ctx context.Context, key string) error {
	return c.store.Delete(ctx, c.key(key))
}

// Has reports whether key exists and is unexpired.
func (c *Cache[V]) Has(ctx context.Context, key string) (bool, error) {
	return c.store.Has(ctx, c.key(key))
}

// Clear removes every key under this cache's prefix. With an empty prefix it
// clears the whole Store.
func (c *Cache[V]) Clear(ctx context.Context) error {
	return c.store.DeletePrefix(ctx, c.prefix)
}

// GetOrSet returns the cached value or computes it via fn on a miss,
// deduplicating concurrent misses so fn runs once per key. The value is cached
// best-effort with the TTL fn returns; load errors are not cached.
//
// Only a genuine miss (ErrNotFound) invokes fn. A Store failure or an
// unreadable cached entry is surfaced as an error rather than masked as a
// miss: errors already matching a cache sentinel pass through unchanged, and
// any other Store error is wrapped with ErrStore (unwrap for the original).
func (c *Cache[V]) GetOrSet(ctx context.Context, key string, fn func(context.Context) (V, time.Duration, error)) (V, error) {
	v, err := c.Get(ctx, key)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, ErrNotFound) {
		var zero V
		if errors.Is(err, ErrClosed) || errors.Is(err, ErrMarshal) || errors.Is(err, ErrUnmarshal) {
			return zero, err
		}
		return zero, errors.Join(ErrStore, err)
	}
	v, _, err = c.sf.Do(ctx, c.key(key), func(ctx context.Context) (V, error) {
		val, ttl, ferr := fn(ctx)
		if ferr != nil {
			var zero V
			return zero, ferr
		}
		_ = c.Set(ctx, key, val, ttl)
		return val, nil
	})
	return v, err
}
