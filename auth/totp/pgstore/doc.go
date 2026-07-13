// Package pgstore is the Postgres implementation of totp.Store: one
// forge_totp table keyed (tenant, subject), with MarkUsed and
// ConsumeBackup as single conditional UPDATEs so replay and backup-code
// races resolve in the database. The DDL ships as an embedded goose
// migration in Migrations; apply it via data/migration under its own
// version table (e.g. "forge_totp_schema").
package pgstore
