// Package pgsink persists auditlog events to Postgres and serves the
// read side of the trail: the append-only insert, tenant-isolated
// keyset-paginated List (the query behind a B2B audit-trail UI), and the
// chain Verify pass for tamper-evident streams. It implements
// auditlog.Sink and auditlog.ChainHead, so a WithChain recorder resumes
// its per-stream hash chain across process restarts.
//
// Apply Migrations before first use (its own goose version table keeps it
// independent of application migrations):
//
//	err := migration.New(pgsink.Migrations, migration.WithTable("forge_auditlog_schema")).Up(ctx, db)
//
//	sink := pgsink.New(pool)
//	rec := auditlog.New(sink, auditlog.WithChain())
//
// The audit-trail page is one List call — keyset pagination over the
// time-ordered UUIDv7 id, so deep pages cost the same as the first:
//
//	page, next, err := sink.List(ctx, pgsink.Filter{Tenant: "org_7", Limit: 50})
//	more, _, err := sink.List(ctx, pgsink.Filter{Tenant: "org_7", Cursor: next})
//
// Multi-tenant applications confine reads with the same fail-closed hook
// the recorder uses:
//
//	sink := pgsink.New(pool, pgsink.WithScope(func(ctx context.Context) (string, error) {
//		return tenantFromCtx(ctx), nil // empty or error aborts the read
//	}))
//
// A compliance audit is one Verify call per stream; compare the returned
// head against an externally anchored copy to also catch tail truncation
// and full-suffix rewrites:
//
//	n, head, err := sink.Verify(ctx, "org_7") // ErrChainBroken names the first bad event
package pgsink
