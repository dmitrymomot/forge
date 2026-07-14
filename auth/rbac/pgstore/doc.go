// Package pgstore is the Postgres driver for rbac.Store: subject→role
// assignments in forge_rbac_assignments, scoped by tenant. Apply Migrations
// (under its own version table) before use; the pool's lifecycle is the
// caller's. See auth/rbac for the engine and the access.Decider adapter.
package pgstore
