// Package pgstore is the Postgres apikey.Store driver over pgx. The DDL
// (forge_api_keys) ships as an embedded goose migration in Migrations;
// apply it via data/migration under its own version table before first
// use:
//
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_apikey_schema")).Up(ctx, db)
//
// GetByHash — the verification hot path — is a single point lookup on the
// unique hash index. Zero time.Time fields map to SQL NULL and back.
package pgstore
