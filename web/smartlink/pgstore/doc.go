// Package pgstore is the Postgres smartlink.Store driver over pgx. The DDL
// (forge_smart_links) ships as an embedded goose migration in Migrations;
// pass it to data/migration and hand the result to postgres.WithMigrator so
// it applies under its own version table before the pool is returned.
//
// Every mutator is a single statement with the tenant predicate inline, so
// the existence check and the tenant check are atomic with the write. Zero
// time.Time fields map to SQL NULL and back.
//
// # Usage
//
//	import (
//		"context"
//		"log/slog"
//		"time"
//
//		"github.com/dmitrymomot/forge/data/migration"
//		"github.com/dmitrymomot/forge/data/postgres"
//		"github.com/dmitrymomot/forge/web/smartlink"
//		"github.com/dmitrymomot/forge/web/smartlink/pgstore"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		pool, err := postgres.Open(ctx,
//			postgres.WithConfig(postgres.DefaultConfig()),
//			postgres.WithMigrator(migration.New(pgstore.Migrations, migration.WithTable("forge_smartlink_schema"))),
//		)
//		if err != nil {
//			slog.Error("db open failed", "err", err)
//			return
//		}
//		defer postgres.Close(pool, slog.Default())
//
//		store := pgstore.New(pool)
//		if err := store.Create(ctx, smartlink.Link{
//			Code:      "abc123",
//			Target:    "https://example.com/",
//			CreatedAt: time.Now().UTC(),
//		}); err != nil {
//			slog.Error("create failed", "err", err)
//			return
//		}
//
//		link, err := store.Get(ctx, "abc123")
//		if err != nil {
//			slog.Error("get failed", "err", err)
//			return
//		}
//		slog.Info("link", "target", link.Target)
//	}
package pgstore
