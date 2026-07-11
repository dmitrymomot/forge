// Package pgstore is a Postgres table-lease implementation of lock.Store. It
// gives TTL leases, monotonic fencing tokens (a sequence), and refresh, all
// compared against the database's own now() so multi-node clock skew cannot
// mis-expire a lease. It works through any connection pooler.
//
// The DDL (forge_locks + forge_locks_fence_seq) ships as an embedded goose
// migration in Migrations; apply it via data/migration under its own version
// table (e.g. "forge_lock_schema").
//
// # Usage
//
//	db := stdlib.OpenDBFromPool(pool)
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_lock_schema")).Up(ctx, db)
//	l := lock.New(pgstore.New(pool), lock.WithTTL(30*time.Second))
package pgstore
