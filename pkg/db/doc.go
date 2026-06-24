// Package db provides PostgreSQL database utilities optimized for SaaS applications.
//
// This package wraps [github.com/jackc/pgx/v5/pgxpool] to provide connection pooling,
// health checks, and database migrations with sensible defaults for production workloads.
//
// # Features
//
//   - Connection pooling with configurable limits and timeouts
//   - Automatic retry logic with linear backoff during startup
//   - Health check function compatible with standard health check interfaces
//   - Database migrations using [github.com/pressly/goose/v3]
//   - Environment-based configuration for deployment convenience
//
// # Configuration
//
// [Config] is populated from environment variables via the struct tags below.
// The env-tag names are relative; consumers typically apply a prefix such as
// DATABASE_ when loading (e.g. DATABASE_URL maps to Config.URL):
//
//	URL                - PostgreSQL connection URL (required)
//	MAX_CONNS          - Maximum open connections (default: 10)
//	MIN_CONNS          - Minimum idle connections (default: 5)
//	HEALTH_CHECK_PERIOD - Health check interval (default: 1m)
//	MAX_CONN_IDLE_TIME - Maximum connection idle time (default: 10m)
//	MAX_CONN_LIFETIME  - Maximum connection lifetime (default: 30m)
//	RETRY_ATTEMPTS     - Connection retry attempts (default: 3)
//	RETRY_INTERVAL     - Base retry interval, applied linearly (default: 5s)
//
// Migrations are tracked in a fixed "schema_migrations" table; this is not
// configurable.
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
//		"github.com/dmitrymomot/forge/pkg/db"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		pool, err := db.Open(ctx, db.Config{
//			URL:      os.Getenv("DATABASE_URL"),
//			MaxConns: 10,
//			MinConns: 5,
//		})
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer pool.Close()
//	}
//
// # Health Checks
//
// The [Healthcheck] function returns a closure suitable for health check endpoints:
//
//	import (
//		"context"
//		"net/http"
//
//		"github.com/dmitrymomot/forge/pkg/db"
//		"github.com/jackc/pgx/v5/pgxpool"
//	)
//
//	func healthHandler(pool *pgxpool.Pool) http.HandlerFunc {
//		healthFn := db.Healthcheck(pool)
//		return func(w http.ResponseWriter, r *http.Request) {
//			if err := healthFn(r.Context()); err != nil {
//				w.WriteHeader(http.StatusServiceUnavailable)
//				return
//			}
//			w.WriteHeader(http.StatusOK)
//		}
//	}
//
// # Transactions
//
// The [WithTx] helper provides automatic transaction management with rollback on error:
//
//	import (
//		"context"
//
//		"github.com/dmitrymomot/forge/pkg/db"
//	)
//
//	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
//		// Execute queries using tx
//		return tx.QueryRow(ctx, "SELECT 1").Scan(&result)
//	})
//	if err != nil {
//		// Transaction was rolled back automatically
//	}
//
// # Migrations
//
// Run database migrations using embedded SQL files:
//
//	import (
//		"context"
//		"embed"
//		"log/slog"
//
//		"github.com/dmitrymomot/forge/pkg/db"
//	)
//
//	//go:embed *.sql
//	var migrations embed.FS
//
//	err := db.Migrate(ctx, pool, migrations, logger)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Error Handling
//
// The package defines sentinel errors for common failure modes:
//
//   - [ErrFailedToParseDBConfig] - Invalid connection string format
//   - [ErrFailedToOpenDBConnection] - Connection failed after all retries
//   - [ErrHealthcheckFailed] - Database ping failed
//   - [ErrSetDialect] - Migration dialect configuration error
//   - [ErrApplyMigrations] - Migration execution failed
//
// Errors are wrapped using [errors.Join] to preserve the original error context.
package db
