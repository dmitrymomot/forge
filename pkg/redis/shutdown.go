package redis

import (
	"context"
	"io"
)

// Shutdown returns a function that gracefully closes the Redis client.
// Use with forge.WithShutdownHook().
//
// Example:
//
//	err := forge.Run(
//	    forge.RunConfig{},
//	    forge.WithFallback(app),
//	    forge.WithShutdownHook(redis.Shutdown(client)),
//	)
func Shutdown(client io.Closer) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return client.Close()
	}
}
