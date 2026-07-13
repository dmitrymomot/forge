// Package pgstore provides the Postgres rng.Store over pgx.
//
// Apply Migrations before first use:
//
//	db := stdlib.OpenDBFromPool(pool)
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_rng_schema")).Up(ctx, db)
//	store := pgstore.New(pool)
//
// The forge_rng_seeds table holds plaintext server seeds until reveal —
// treat it as secret material; at-rest encryption is a storage concern
// (disk encryption, pgcrypto) outside this package.
package pgstore
