// Package pgstore is the Postgres invite.Store driver over pgx. The DDL
// (forge_invites) ships as an embedded goose migration in Migrations;
// apply it via data/migration under its own version table before first
// use:
//
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_invite_schema")).Up(ctx, db)
//
// GetByHash — the accept path — is a single point lookup on the unique
// hash index. Accept, Revoke, and Rotate are conditional single-row
// UPDATEs, so single-use holds under concurrent accepts without
// transactions. Zero time.Time fields map to SQL NULL and back.
package pgstore
