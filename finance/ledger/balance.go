package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/money"
)

// Balance is an account's position. Available = Balance − Held; for floored
// accounts it never drops below the floor. For floor-free accounts Balance
// derives from the latest snapshot plus postings since, and Held from open
// hold rows.
type Balance struct {
	Balance   money.Money
	Held      money.Money
	Available money.Money
}

// balanceSQL reads either side in one statement (one MVCC snapshot, so the
// derived branch is internally consistent): materialized columns for floored
// accounts; snapshot + signed sum of postings at-or-past the snapshot horizon
// plus SUM(open holds) for floor-free ones. The UNION ALL arms of the delta
// each ride one (account, txid) index.
const balanceSQL = `
SELECT
    a.currency,
    CASE WHEN a.floor IS NOT NULL THEN a.balance
         ELSE COALESCE(s.balance, 0) + COALESCE((
             SELECT SUM(x) FROM (
                 SELECT -amount AS x FROM ledger_postings
                 WHERE src_account = a.id AND txid >= COALESCE(s.txid, '0'::xid8)
                 UNION ALL
                 SELECT amount FROM ledger_postings
                 WHERE dst_account = a.id AND txid >= COALESCE(s.txid, '0'::xid8)
             ) AS d), 0)
    END::text AS balance,
    CASE WHEN a.floor IS NOT NULL THEN a.held
         ELSE COALESCE((SELECT SUM(amount) FROM ledger_holds
                        WHERE account = a.id AND status = 'open'), 0)
    END::text AS held
FROM ledger_accounts a
LEFT JOIN ledger_snapshots s ON s.account = a.id AND a.floor IS NULL
WHERE a.id = $1 AND a.tenant = $2`

// Balance reads an account's balance, held, and available amounts. Floored
// accounts read their materialized columns; floor-free accounts derive from
// the snapshots table plus postings since the snapshot horizon — no account
// row is locked either way, so reads never contend with the posting hot
// path.
func (l *Ledger) Balance(ctx context.Context, db DB, account id.UUID) (Balance, error) {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Balance{}, err
	}
	var code, balText, heldText string
	err = db.QueryRow(ctx, balanceSQL, account, tenant).Scan(&code, &balText, &heldText)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Balance{}, fmt.Errorf("%w: %s", ErrAccountNotFound, account)
		}
		return Balance{}, fmt.Errorf("ledger: balance of %s: %w", account, err)
	}
	return newBalance(balText, heldText, code)
}

// newBalance assembles a Balance from numeric::text columns.
func newBalance(balText, heldText, code string) (Balance, error) {
	bal, err := parseMoney(balText, code)
	if err != nil {
		return Balance{}, err
	}
	held, err := parseMoney(heldText, code)
	if err != nil {
		return Balance{}, err
	}
	avail, err := bal.Sub(held)
	if err != nil {
		return Balance{}, fmt.Errorf("ledger: available: %w", err)
	}
	return Balance{Balance: bal, Held: held, Available: avail}, nil
}

// snapshotSQL advances an account's snapshot in one statement. The horizon is
// pg_snapshot_xmin(pg_current_snapshot()): every transaction below it has
// finished, so postings with txid < horizon are either visible now or aborted
// — none are in flight. The new balance is the previous snapshot plus the
// signed delta over [prev horizon, new horizon); readers add postings with
// txid >= horizon, so no posting is ever counted twice or missed.
const snapshotSQL = `
WITH acct AS (
    SELECT id FROM ledger_accounts WHERE id = $1 AND tenant = $2
), prev AS (
    SELECT balance, txid FROM ledger_snapshots WHERE account = $1
), horizon AS (
    SELECT pg_snapshot_xmin(pg_current_snapshot()) AS h
), delta AS (
    SELECT COALESCE(SUM(x), 0) AS d FROM (
        SELECT -amount AS x FROM ledger_postings, horizon
        WHERE src_account = $1
          AND txid >= COALESCE((SELECT txid FROM prev), '0'::xid8) AND txid < horizon.h
        UNION ALL
        SELECT amount FROM ledger_postings, horizon
        WHERE dst_account = $1
          AND txid >= COALESCE((SELECT txid FROM prev), '0'::xid8) AND txid < horizon.h
    ) AS t
), ins AS (
    INSERT INTO ledger_snapshots (account, balance, txid, created_at)
    SELECT acct.id, COALESCE((SELECT balance FROM prev), 0) + (SELECT d FROM delta),
           (SELECT h FROM horizon), $3
    FROM acct
    ON CONFLICT (account) DO UPDATE
        SET balance = EXCLUDED.balance, txid = EXCLUDED.txid, created_at = EXCLUDED.created_at
    RETURNING account
)
SELECT EXISTS (SELECT 1 FROM acct), EXISTS (SELECT 1 FROM ins)`

// Snapshot advances the account's balance snapshot: the verified cache that
// keeps floor-free balance reads O(postings since last snapshot) instead of
// O(all postings). Run it periodically (a scheduler job per hot floor-free
// account); any DB handle works — the statement is atomic on its own.
// Snapshotting a floored account is valid and merely maintains the same
// derivable cache.
func (l *Ledger) Snapshot(ctx context.Context, db DB, account id.UUID) error {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return err
	}
	var found, wrote bool
	err = db.QueryRow(ctx, snapshotSQL, account, tenant, l.clk.Now().UTC()).Scan(&found, &wrote)
	if err != nil {
		return fmt.Errorf("ledger: snapshot %s: %w", account, err)
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrAccountNotFound, account)
	}
	return nil
}

// Drift compares an account's reported position against a full recompute
// from the postings and open holds. Reported is what Balance would return
// (materialized columns or snapshot-derived); Computed is the ground truth.
type Drift struct {
	Reported     money.Money
	Computed     money.Money
	ReportedHeld money.Money
	ComputedHeld money.Money
	Account      id.UUID
	Drifted      bool
}

// driftSQL recomputes balance and held from scratch and reads the reported
// values in the same statement — one MVCC snapshot, so an in-flight posting
// can never manufacture false drift.
const driftSQL = `
SELECT
    a.currency,
    CASE WHEN a.floor IS NOT NULL THEN a.balance
         ELSE COALESCE(s.balance, 0) + COALESCE((
             SELECT SUM(x) FROM (
                 SELECT -amount AS x FROM ledger_postings
                 WHERE src_account = a.id AND txid >= COALESCE(s.txid, '0'::xid8)
                 UNION ALL
                 SELECT amount FROM ledger_postings
                 WHERE dst_account = a.id AND txid >= COALESCE(s.txid, '0'::xid8)
             ) AS d), 0)
    END::text AS reported_balance,
    CASE WHEN a.floor IS NOT NULL THEN a.held
         ELSE COALESCE((SELECT SUM(amount) FROM ledger_holds
                        WHERE account = a.id AND status = 'open'), 0)
    END::text AS reported_held,
    (COALESCE((SELECT SUM(amount) FROM ledger_postings WHERE dst_account = a.id), 0)
   - COALESCE((SELECT SUM(amount) FROM ledger_postings WHERE src_account = a.id), 0))::text AS computed_balance,
    COALESCE((SELECT SUM(amount) FROM ledger_holds
              WHERE account = a.id AND status = 'open'), 0)::text AS computed_held
FROM ledger_accounts a
LEFT JOIN ledger_snapshots s ON s.account = a.id AND a.floor IS NULL
WHERE a.id = $1 AND a.tenant = $2`

// CheckDrift verifies one account: materialized (or snapshot-derived)
// balances are caches, postings are the truth, and this is the job that
// proves the cache. A Drifted result is a bug or manual intervention —
// corrections are forward postings, never balance edits. Sweep all accounts
// by paging Accounts and calling CheckDrift per id.
func (l *Ledger) CheckDrift(ctx context.Context, db DB, account id.UUID) (Drift, error) {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Drift{}, err
	}
	var code, repBal, repHeld, compBal, compHeld string
	err = db.QueryRow(ctx, driftSQL, account, tenant).Scan(&code, &repBal, &repHeld, &compBal, &compHeld)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Drift{}, fmt.Errorf("%w: %s", ErrAccountNotFound, account)
		}
		return Drift{}, fmt.Errorf("ledger: drift check %s: %w", account, err)
	}
	d := Drift{Account: account}
	if d.Reported, err = parseMoney(repBal, code); err != nil {
		return Drift{}, err
	}
	if d.ReportedHeld, err = parseMoney(repHeld, code); err != nil {
		return Drift{}, err
	}
	if d.Computed, err = parseMoney(compBal, code); err != nil {
		return Drift{}, err
	}
	if d.ComputedHeld, err = parseMoney(compHeld, code); err != nil {
		return Drift{}, err
	}
	balEq, err := d.Reported.Equal(d.Computed)
	if err != nil {
		return Drift{}, fmt.Errorf("ledger: drift compare: %w", err)
	}
	heldEq, err := d.ReportedHeld.Equal(d.ComputedHeld)
	if err != nil {
		return Drift{}, fmt.Errorf("ledger: drift compare: %w", err)
	}
	d.Drifted = !balEq || !heldEq
	return d, nil
}
