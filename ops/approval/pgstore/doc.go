// Package pgstore is the Postgres approval.Store driver over pgx. The DDL
// (forge_approval_requests) ships as an embedded goose migration in
// Migrations; apply it via data/migration under its own version table
// before first use:
//
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_approval_schema")).Up(ctx, db)
//
// Update is a compare-and-swap enforced by Postgres itself (an UPDATE ...
// WHERE id = $1 AND version = $2), not by an in-process lock, so it holds
// under concurrent transactions from separate processes. payload and
// decisions are stored as json, not jsonb, so a payload a caller hashes
// round-trips byte-identical. The pool's lifecycle is the caller's.
package pgstore
