// Package pgstore is the Postgres smartlink.Store driver over pgx. The DDL
// (forge_smart_links) ships as an embedded goose migration in Migrations;
// apply it via data/migration under its own version table before first use:
//
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_smartlink_schema")).Up(ctx, db)
//
// Every mutator is a single statement with the tenant predicate inline, so
// the existence check and the tenant check are atomic with the write. Zero
// time.Time fields map to SQL NULL and back.
package pgstore
