package redis

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

// Healthcheck returns a closure that validates Redis connectivity for health endpoints.
// Compatible with standard health check interfaces that expect func(context.Context) error.
func Healthcheck(client redis.UniversalClient) func(context.Context) error {
	return func(ctx context.Context) error {
		if client == nil {
			return ErrHealthcheckFailed
		}
		if err := client.Ping(ctx).Err(); err != nil {
			return errors.Join(ErrHealthcheckFailed, err)
		}
		return nil
	}
}
