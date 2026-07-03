package redis

import (
	"errors"

	goredis "github.com/redis/go-redis/v9"
)

// Sentinel errors returned by this package, wrapped around the underlying driver
// error. Match them with errors.Is. They are single-line and carry no embedded
// stacks or multi-line blobs, per the framework's structured-logging rule.
var (
	// ErrInvalidConfig is returned (joined) by Validate and Open when a Config
	// field or an option has an unusable value.
	ErrInvalidConfig = errors.New("redis: invalid config")
	// ErrConnect is returned by Open when the client cannot reach the server
	// after exhausting the bounded connect-retry loop.
	ErrConnect = errors.New("redis: connect failed")
	// ErrHealthcheck is returned by the Healthcheck closure when a PING fails.
	ErrHealthcheck = errors.New("redis: healthcheck failed")
)

// IsNil reports whether err is (or wraps) goredis.Nil, the sentinel go-redis returns
// for a key that does not exist — a cache miss, not a failure. App code branches with
// IsNil instead of importing the driver to compare against goredis.Nil.
func IsNil(err error) bool {
	return errors.Is(err, goredis.Nil)
}
