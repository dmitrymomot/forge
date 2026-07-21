// Package pgstore is the Postgres driver for acl.Store: per-subject,
// per-resource grant/deny entries in forge_acl_entries, scoped by tenant.
// Apply Migrations (under its own version table) before use; the pool's
// lifecycle is the caller's. See auth/acl for the engine and the
// access.Decider adapter.
package pgstore
