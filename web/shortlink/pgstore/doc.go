// Package pgstore is the Postgres shortlink.Store driver: one
// forge_short_links table keyed by code, resolved with a single
// primary-key lookup.
//
//	pool, _ := postgres.Open(ctx, postgres.WithConfig(cfg))
//	db := stdlib.OpenDBFromPool(pool)
//	err := migration.New(pgstore.Migrations, migration.WithTable("forge_shortlink_schema")).Up(ctx, db)
//
//	mgr := shortlink.New(pgstore.New(pool))
package pgstore
