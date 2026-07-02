package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Store is a byte-level key/value backend with TTL. Implementations are
// standalone instances whose lifecycle (background goroutines, connections) is
// owned by the caller via Close. TTL semantics: >0 expires after the duration,
// 0 means the store's own default (none for the memory store), <0 never expires.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Has(ctx context.Context, key string) (bool, error)
	DeletePrefix(ctx context.Context, prefix string) error
	Close() error
}

// Marshaler serializes cache values to and from bytes.
type Marshaler[V any] interface {
	Marshal(v V) ([]byte, error)
	Unmarshal(data []byte) (V, error)
}

type jsonMarshaler[V any] struct{}

func (jsonMarshaler[V]) Marshal(v V) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, errors.Join(ErrMarshal, err)
	}
	return b, nil
}

func (jsonMarshaler[V]) Unmarshal(data []byte) (V, error) {
	var v V
	if err := json.Unmarshal(data, &v); err != nil {
		return v, errors.Join(ErrUnmarshal, err)
	}
	return v, nil
}
