package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Store is a byte-level key/value backend with optional per-entry TTL.
// Implementations are standalone instances whose lifecycle (background
// goroutines, connections) is owned by the caller via Close.
//
// Set is configured with SetOption values: WithTTL sets an expiry (no option
// means no expiry), WithSetNonExist makes the write conditional and returns
// ErrExists when the key already exists. Implementations resolve options with
// ApplySetOptions so every backend behaves identically.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, opts ...SetOption) error
	Delete(ctx context.Context, key string) error
	Has(ctx context.Context, key string) (bool, error)
	DeletePrefix(ctx context.Context, prefix string) error
	Close() error
}

// SetOptions holds the resolved settings for one Set call. It is exported
// because Store implementations in sibling packages (e.g. cache/redis) apply
// SetOption values and read the result. The zero value means: never expire,
// overwrite an existing key.
type SetOptions struct {
	// TTL expires the entry after the duration. A value <= 0 means no expiry.
	TTL time.Duration
	// OnlyIfNew stores the value only when the key is absent or expired; on a
	// live key Set writes nothing and returns ErrExists.
	OnlyIfNew bool
}

// SetOption configures a single Set call.
type SetOption func(*SetOptions)

// WithTTL expires the written entry after d. Without it the entry never
// expires; a non-positive d is equivalent to omitting the option.
func WithTTL(d time.Duration) SetOption { return func(o *SetOptions) { o.TTL = d } }

// WithSetNonExist stores the value only if the key is absent or expired. On a
// live key Set writes nothing and returns ErrExists. Combined with WithTTL it
// is an atomic claim-with-lease (set-if-absent + expiry).
func WithSetNonExist() SetOption { return func(o *SetOptions) { o.OnlyIfNew = true } }

// ApplySetOptions resolves opts into a SetOptions so every backend applies the
// options identically.
func ApplySetOptions(opts ...SetOption) SetOptions {
	var o SetOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
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
