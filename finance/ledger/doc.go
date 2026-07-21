// Package ledger is a double-entry money ledger over core/money,
// Postgres-native by design: every invariant is a SQL predicate executed
// inside the caller's pgx.Tx, so a posting commits or vanishes together with
// the caller's own rows (sqlc repositories, eventbus.Seen) in one
// transaction. There is no storage seam — a faithful second implementation
// would be a second ledger. The package owns its schema via the embedded
// Migrations; consumers never write ledger tables, reads go through the
// query API.
//
// Postings are pairwise (src, dst, amount, one currency): balanced by
// construction, one row per movement, with GroupRef correlating the parts of
// a multi-part event (deposit = wallet 97 + fee 3) and Adjusts
// back-referencing the posting a forward correction amends. Every write is
// idempotent by external ref — a replay returns the original row, a replay
// with different parameters is ErrRefConflict. Rows change status; money
// history only ever grows.
//
// Holds are rows, not shadow accounts: Hold reserves available balance
// without a posting, Settle writes the single real posting (full or partial
// via SettleAmount), Void writes none. Void after Settle is
// ErrAlreadySettled — later corrections are forward postings with Adjusts.
// Expiry (ExpiresAt + ExpiredHolds) is mechanism; the sweep policy is the
// consumer's: a bet hold auto-voids, a CPA hold auto-settles.
//
// Accounts are an explicit registry — EnsureAccount, unique (tenant, owner,
// purpose, currency) — never created implicitly by a posting. The floor
// drives locking: floored accounts (default, floor zero) carry materialized
// balance/held moved under sorted row locks with the floor predicate in the
// UPDATE's WHERE (zero rows = ErrInsufficientFunds, the transaction stays
// usable); floor-free accounts (WithoutFloor — house/mint) are never locked
// or updated on the hot path, their balances derive from the snapshots table
// plus postings since the snapshot's transaction-id horizon. Snapshot
// advances that cache; CheckDrift proves it against a full recompute.
//
// Multi-tenant apps install a scope hook via WithScope; every operation then
// resolves the tenant from the context and fails closed (ErrScopeMissing)
// when it cannot. Single-tenant apps skip the option and pay no ceremony.
//
// # Usage
//
//	l := ledger.New()
//
//	// boot: apply the ledger schema under its own version table
//	pool, _ := postgres.Open(ctx, postgres.WithConfig(cfg),
//		postgres.WithMigrator(migration.New(ledger.Migrations, migration.WithTable("ledger_migrations"))))
//
//	// registry: a player wallet (floored at zero) and the house (floor-free)
//	tx, _ := pool.Begin(ctx)
//	wallet, _ := l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: playerID, Purpose: "wallet", Currency: money.EUR})
//	house, _ := l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: "house", Purpose: "casino", Currency: money.EUR},
//		ledger.WithoutFloor())
//
//	// bet: hold the stake, then settle the loss to the house
//	stake := money.FromMinor(500, money.EUR)
//	_, _ = l.Hold(ctx, tx, ledger.Hold{Ref: betID, Account: wallet.ID, Amount: stake})
//	_, _ = l.Settle(ctx, tx, betID, house.ID)
//	_ = tx.Commit(ctx)
//
//	bal, _ := l.Balance(ctx, pool, wallet.ID) // Balance, Held, Available
//	_ = bal
//
// Not owned here: limit rules (domain code in the same tx — the ledger
// supplies the tx and available-balance primitives), FX conversion math
// (finance/fxrate records rates), statements and invoices (readers over the
// query API), and process history — deposits, game rounds, and provider
// calls including rejected ones live in consumer tables with their own
// balance snapshots, linked 1:1 by posting ref. The ledger records money
// that moved, never attempts.
package ledger
