package redis

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// GetJSON fetches key and json.Unmarshals it into a T. On a cache miss it returns the
// zero T together with the goredis.Nil error, so callers can branch with IsNil(err);
// any other Get or Unmarshal failure is returned with the zero T. It operates over
// goredis.Cmdable, so it works against a client, a pipeline, or a transaction.
func GetJSON[T any](ctx context.Context, c goredis.Cmdable, key string) (T, error) {
	var v T
	b, err := c.Get(ctx, key).Bytes()
	if err != nil {
		return v, err // goredis.Nil on a miss; the driver error otherwise
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	return v, nil
}

// SetJSON json.Marshals v and stores it at key with the given ttl (0 = no expiry,
// per go-redis Set semantics). It operates over goredis.Cmdable, so it works against
// a client, a pipeline, or a transaction.
func SetJSON(ctx context.Context, c goredis.Cmdable, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, ttl).Err()
}
