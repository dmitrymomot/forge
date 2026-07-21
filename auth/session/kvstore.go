package session

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// kvStore adapts the framework-wide TTL-KV seam (cache.Store) into a session
// Store. Records ride the backend's native TTL, keyed by a digest of the
// token so the bearer credential never appears in backend keys (Redis SCAN,
// dashboards). Plain KV cannot list by user, so there is no UserIndex —
// use pgstore when multi-device management matters.
type kvStore struct {
	kv cache.Store
}

// NewKVStore returns a Store over kv. Use a durable backend (cache/redis) —
// cache's LRU memory store evicts live sessions under pressure; for
// tests/dev prefer NewMemoryStore.
func NewKVStore(kv cache.Store) (Store, error) {
	if kv == nil {
		return nil, fmt.Errorf("%w: kv store is required", ErrInvalidConfig)
	}
	return &kvStore{kv: kv}, nil
}

func kvKey(token string) string {
	return "session:" + hex.EncodeToString(digest.SHA256([]byte(token)))
}

// Save upserts rec with a TTL matching its deadline; the token is returned
// unchanged. An already-expired record is deleted instead of stored, so a
// backend treating TTL<=0 as "no expiry" can never hold an eternal session.
func (s *kvStore) Save(ctx context.Context, token string, rec Record) (string, error) {
	ttl := time.Until(rec.ExpiresAt)
	if ttl <= 0 {
		if err := s.kv.Delete(ctx, kvKey(token)); err != nil {
			return "", err
		}
		return token, nil
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	if err := s.kv.Set(ctx, kvKey(token), b, cache.WithTTL(ttl)); err != nil {
		return "", err
	}
	return token, nil
}

// Load returns the record for token, or ErrNotFound.
func (s *kvStore) Load(ctx context.Context, token string) (Record, error) {
	b, err := s.kv.Get(ctx, kvKey(token))
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return Record{}, ErrNotFound
		}
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Delete removes the record for token; absent tokens are a no-op.
func (s *kvStore) Delete(ctx context.Context, token string) error {
	return s.kv.Delete(ctx, kvKey(token))
}
