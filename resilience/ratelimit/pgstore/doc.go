// Package pgstore is a durable Postgres implementation of ratelimit.Store (the
// counter seam shared by ratelimit and quota). It is the recommended backend
// for quota gauges, which need non-expiring counters the memory store can prune.
//
// The DDL (forge_ratelimit_counters: key text PK, val bigint, expires_at
// timestamptz NULL) ships as an embedded goose migration in Migrations; apply
// it via data/migration under its own version table (e.g. "forge_ratelimit_schema").
// Non-goose shops can copy that DDL into their own migration tool.
//
// # Usage
//
//	db := stdlib.OpenDBFromPool(pool)
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_ratelimit_schema")).Up(ctx, db)
//	store := pgstore.New(pool)
//	m := quota.New(store, quota.Gauge())
package pgstore
