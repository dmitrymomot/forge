// Package pgstore is the Postgres session.Store + session.UserIndex driver
// over pgx — the backing for multi-device management ("log out other
// devices", GDPR deletion). Tokens are persisted only as SHA-256 digests, so
// a database leak exposes no usable session credentials.
//
// The DDL (forge_sessions) ships as an embedded goose migration in
// Migrations; apply it via data/migration under its own version table before
// first use:
//
//	_ = migration.New(pgstore.Migrations, migration.WithTable("forge_session_schema")).Up(ctx, db)
//
// Expired rows are deleted lazily on Load; schedule DeleteExpired
// (async/scheduler or cron) to keep the table bounded:
//
//	n, err := store.DeleteExpired(ctx, time.Now())
package pgstore
