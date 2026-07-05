package redis

import (
	"context"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	forgeredis "github.com/dmitrymomot/forge/data/redis"
	"github.com/dmitrymomot/forge/resilience/cache"
)

type store struct {
	client goredis.UniversalClient
}

// NewStore returns a cache.Store backed by client. Store.Close is a no-op: the
// caller owns the client and must close it.
func NewStore(client goredis.UniversalClient) cache.Store {
	return &store{client: client}
}

func (s *store) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := s.client.Get(ctx, key).Bytes()
	if forgeredis.IsNil(err) {
		return nil, cache.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *store) Set(ctx context.Context, key string, val []byte, opts ...cache.SetOption) error {
	o := cache.ApplySetOptions(opts...)
	var ttl time.Duration
	if o.TTL > 0 {
		ttl = o.TTL // SetArgs treats TTL <= 0 as "no expiry"
	}
	if o.OnlyIfNew {
		// SET key val NX [PX ttl] — the modern replacement for SETNX. On a
		// present key the server returns a null reply, surfaced as redis.Nil.
		err := s.client.SetArgs(ctx, key, val, goredis.SetArgs{Mode: "NX", TTL: ttl}).Err()
		if forgeredis.IsNil(err) {
			return cache.ErrExists
		}
		return err
	}
	return s.client.Set(ctx, key, val, ttl).Err()
}

func (s *store) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func (s *store) Has(ctx context.Context, key string) (bool, error) {
	n, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// escapeGlob escapes Redis glob metacharacters so a prefix is matched
// literally by SCAN MATCH. Without this, a prefix containing * ? [ ] \ would be
// interpreted as a pattern, diverging from the in-memory store's literal
// strings.HasPrefix and breaking prefix isolation.
func escapeGlob(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *store) DeletePrefix(ctx context.Context, prefix string) error {
	pattern := escapeGlob(prefix) + "*"
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Close is a no-op; the caller owns the client's lifecycle.
func (s *store) Close() error { return nil }
