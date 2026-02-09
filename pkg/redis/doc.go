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
// All settings are configured via the Config struct:
//
//   - URL — Redis connection URL (required)
//   - PoolSize — Maximum number of connections (default: 10)
//   - MinIdleConns — Minimum idle connections (default: 5)
//   - MaxIdleTime — Maximum connection idle time (default: 10m)
//   - MaxActiveTime — Maximum connection lifetime (default: 30m)
//   - RetryAttempts — Retry attempts (default: 3)
//   - RetryInterval — Retry base interval (default: 5s)
//   - ReadTimeout — Read operation timeout (default: 3s)
//   - WriteTimeout — Write operation timeout (default: 3s)
//   - DialTimeout — Connection dial timeout (default: 5s)
//
// # Usage
//
// Basic connection setup with Config:
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
//		client, err := redis.Open(ctx, redis.Config{
//			URL:      os.Getenv("REDIS_URL"),
//			PoolSize: 20,
//		})
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
//	client := redis.MustOpen(ctx, redis.Config{URL: redisURL})
//	app := forge.New(forge.AppConfig{})
//
//	if err := forge.Run(
//		forge.RunConfig{},
//		forge.WithFallback(app),
//		forge.WithShutdownHook(redis.Shutdown(client)),
//	); err != nil {
//		log.Fatal(err)
//	}
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
