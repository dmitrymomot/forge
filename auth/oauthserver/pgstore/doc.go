// Package pgstore is the Postgres oauthserver.Store: the oauth_clients
// table behind the client registry. Apply Migrations via data/migration
// (its .sql files sit at the fs root) before use; the pgxpool's lifecycle
// belongs to the caller.
//
//	require.NoError(t, migration.New(pgstore.Migrations,
//	    migration.WithTable("forge_oauthserver_schema")).Up(ctx, db))
//	store := pgstore.New(pool)
//	srv, err := oauthserver.New(signer, store, ...)
package pgstore
