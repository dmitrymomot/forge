// Package redis provides Redis client utilities optimized for SaaS applications.
//
// This package wraps [github.com/redis/go-redis/v9] to provide connection pooling,
// health checks, and graceful shutdown with sensible defaults for production workloads.
//
// # Features
//
//   - Connection pooling with configurable limits and timeouts
//   - Automatic retry logic with exponential backoff during startup
//   - Health check function compatible with standard health check interfaces
//   - Support for redis:// and rediss:// (TLS) URL schemes
//   - Graceful shutdown hook integration with Forge applications
//
// # Configuration
//
// All settings are configured via functional options:
//
//   - WithPoolSize(n int) — Maximum number of connections (default: 10)
//   - WithMinIdleConns(n int) — Minimum idle connections (default: 5)
//   - WithMaxIdleTime(d time.Duration) — Maximum connection idle time (default: 10m)
//   - WithMaxActiveTime(d time.Duration) — Maximum connection lifetime (default: 30m)
//   - WithRetry(attempts int, interval time.Duration) — Retry attempts and base interval (default: 3 attempts, 5s)
//   - WithReadTimeout(d time.Duration) — Read operation timeout (default: 3s)
//   - WithWriteTimeout(d time.Duration) — Write operation timeout (default: 3s)
//   - WithDialTimeout(d time.Duration) — Connection dial timeout (default: 5s)
//
// # Usage
//
// Basic connection setup with functional options:
//
//	import (
//		"context"
//		"log"
//		"os"
//
//		"github.com/dmitrymomot/forge/pkg/redis"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		client, err := redis.Open(ctx, os.Getenv("REDIS_URL"),
//			redis.WithPoolSize(20),
//			redis.WithMinIdleConns(5),
//		)
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer client.Close()
//	}
//
// # Health Checks
//
// The [Healthcheck] function returns a closure suitable for health check endpoints:
//
//	import (
//		"net/http"
//
//		goredis "github.com/redis/go-redis/v9"
//		"github.com/dmitrymomot/forge/pkg/redis"
//	)
//
//	func healthHandler(client goredis.UniversalClient) http.HandlerFunc {
//		healthFn := redis.Healthcheck(client)
//		return func(w http.ResponseWriter, r *http.Request) {
//			if err := healthFn(r.Context()); err != nil {
//				w.WriteHeader(http.StatusServiceUnavailable)
//				return
//			}
//			w.WriteHeader(http.StatusOK)
//		}
//	}
//
// # Graceful Shutdown
//
// Use [Shutdown] with Forge's shutdown hook for graceful client closure:
//
//	import (
//		"github.com/dmitrymomot/forge"
//		"github.com/dmitrymomot/forge/pkg/redis"
//	)
//
//	client := redis.MustOpen(ctx, redisURL)
//	app := forge.New(
//		forge.WithShutdownHook(redis.Shutdown(client)),
//	)
//
// # Error Handling
//
// The package defines sentinel errors for common failure modes:
//
//   - [ErrEmptyConnectionURL] - Empty connection URL provided
//   - [ErrFailedToParseURL] - Invalid connection URL format or scheme
//   - [ErrConnectionFailed] - Connection failed after all retry attempts
//   - [ErrHealthcheckFailed] - Redis ping failed
//
// Errors are wrapped using [errors.Join] to preserve the original error context.
package redis
