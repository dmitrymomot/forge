package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/data/postgres"
)

// HoldStatus is the lifecycle state of a hold.
type HoldStatus string

// Hold lifecycle: open until exactly one of Settle (money moves) or Void
// (nothing moves) resolves it.
const (
	HoldOpen    HoldStatus = "open"
	HoldSettled HoldStatus = "settled"
	HoldVoided  HoldStatus = "voided"
)

// Hold reserves Amount of an account's available balance without a posting.
// On input to Ledger.Hold, Ref, Account, and Amount are required; a non-zero
// ExpiresAt marks the hold for the ExpiredHolds sweep (the ledger itself
// never expires holds — sweep policy is the consumer's). Status, CreatedAt,
// and ResolvedAt are assigned by the ledger.
type Hold struct {
	ExpiresAt  time.Time
	CreatedAt  time.Time
	ResolvedAt time.Time
	Ref        string
	Status     HoldStatus
	Amount     money.Money
	Account    id.UUID
}

// holdSQL opens a hold in one statement: replay gate, conditional-WHERE
// update of the floored account's held (the floor predicate in the WHERE is
// the funds check — zero rows means insufficient), and the hold-row insert
// gated on success. Floor-free accounts get the row without a held update.
const holdSQL = `
WITH existing AS (
    SELECT account, amount::text AS amount, currency, status, expires_at, created_at, resolved_at
    FROM ledger_holds WHERE ref = $1
), acct AS (
    SELECT id, currency, floor FROM ledger_accounts WHERE id = $2 AND tenant = $7
), upd AS (
    UPDATE ledger_accounts a
    SET held = a.held + $3::numeric
    WHERE a.id = $2 AND a.tenant = $7 AND a.floor IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM existing)
      AND a.currency = $4
      AND a.balance - a.held - $3::numeric >= a.floor
    RETURNING a.id
), ok AS (
    SELECT 1 AS yes
    WHERE EXISTS (SELECT 1 FROM upd)
       OR (NOT EXISTS (SELECT 1 FROM existing)
           AND EXISTS (SELECT 1 FROM acct WHERE floor IS NULL AND currency = $4))
), ins AS (
    INSERT INTO ledger_holds (ref, account, amount, currency, status, expires_at, created_at)
    SELECT $1, $2, $3::numeric, $4, 'open', $5, $6
    WHERE EXISTS (SELECT 1 FROM ok)
    RETURNING ref
)
SELECT
    EXISTS (SELECT 1 FROM existing),
    EXISTS (SELECT 1 FROM acct),
    EXISTS (SELECT 1 FROM acct WHERE currency <> $4),
    EXISTS (SELECT 1 FROM ins),
    e.account, e.amount, e.currency, e.status, e.expires_at, e.created_at, e.resolved_at
FROM (SELECT 1) AS one
LEFT JOIN existing e ON true`

// Hold reserves h.Amount on h.Account inside the caller's transaction:
// available (balance − held) drops by the amount, balance is untouched, and
// no posting is written. Money moves once, at Settle; Void releases the
// reservation without a trace in the postings.
//
// Hold is idempotent by Ref: a replay returns the hold in its current state
// (possibly already settled or voided), or ErrRefConflict if the parameters
// differ. Two transactions racing the same new Ref resolve like Post: the
// loser aborts with ErrRefRace.
func (l *Ledger) Hold(ctx context.Context, tx pgx.Tx, h Hold) (Hold, error) {
	if h.Ref == "" {
		return Hold{}, fmt.Errorf("%w: hold ref is required", ErrInvalidRef)
	}
	if !h.Amount.IsPositive() {
		return Hold{}, fmt.Errorf("%w: hold amount must be positive, got %s", ErrInvalidAmount, h.Amount)
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Hold{}, err
	}

	var expires *time.Time
	if !h.ExpiresAt.IsZero() {
		// timestamptz stores microseconds; truncate up front so a replay
		// carrying the same nanosecond-precision value matches the stored row.
		h.ExpiresAt = h.ExpiresAt.UTC().Truncate(time.Microsecond)
		expires = new(h.ExpiresAt)
	}
	now := l.clk.Now().UTC()
	var (
		replay, acctFound, mismatch, inserted bool
		ex                                    existingHold
	)
	err = tx.QueryRow(ctx, holdSQL,
		h.Ref, h.Account, h.Amount.Amount().String(), h.Amount.Currency().Code,
		expires, now, tenant,
	).Scan(&replay, &acctFound, &mismatch, &inserted,
		&ex.account, &ex.amount, &ex.currency, &ex.status, &ex.expiresAt, &ex.createdAt, &ex.resolvedAt)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return Hold{}, fmt.Errorf("%w: ref %q", ErrRefRace, h.Ref)
		}
		return Hold{}, fmt.Errorf("ledger: hold %q: %w", h.Ref, err)
	}

	if replay {
		orig, err := ex.hold(h.Ref)
		if err != nil {
			return Hold{}, err
		}
		if err := matchHoldReplay(orig, h); err != nil {
			return Hold{}, err
		}
		return orig, nil
	}
	switch {
	case !acctFound:
		return Hold{}, fmt.Errorf("%w: %s", ErrAccountNotFound, h.Account)
	case mismatch:
		return Hold{}, fmt.Errorf("%w: hold is %s", ErrCurrencyMismatch, h.Amount.Currency().Code)
	case !inserted:
		return Hold{}, fmt.Errorf("%w: hold %s on %s", ErrInsufficientFunds, h.Amount, h.Account)
	}
	h.Status = HoldOpen
	h.CreatedAt = now
	h.ResolvedAt = time.Time{}
	return h, nil
}

// matchHoldReplay verifies a replayed Hold carries the original parameters.
// h.ExpiresAt was truncated to microseconds before the statement ran, so the
// comparison against the stored timestamptz is exact.
func matchHoldReplay(orig, h Hold) error {
	eq, err := orig.Amount.Equal(h.Amount)
	if err != nil || !eq || orig.Account != h.Account || !orig.ExpiresAt.Equal(h.ExpiresAt) {
		return fmt.Errorf("%w: ref %q", ErrRefConflict, h.Ref)
	}
	return nil
}

// SettleOption configures Settle.
type SettleOption func(*settleConfig)

type settleConfig struct {
	amount money.NullMoney
}

// SettleAmount settles the hold for m instead of the full held amount
// (partial capture: authorize 100, settle 97). The remainder of the hold is
// released. m must be positive, in the hold currency, and at most the held
// amount.
func SettleAmount(m money.Money) SettleOption {
	return func(c *settleConfig) { c.amount = money.NullMoney{Money: m, Valid: true} }
}

// settleSQL resolves an open hold in one statement: the hold row is locked
// first (serializing Settle/Void on the same ref), floored accounts among
// {hold.account, dst} lock in sorted id order, then balance moves, the full
// held amount releases, the hold flips to settled, and the single real
// posting (ref = hold ref) is inserted. No floor check: settling can only
// raise available, so an open hold always settles.
const settleSQL = `
WITH h AS (
    SELECT ref, account, amount, amount::text AS amount_text, currency, status
    FROM ledger_holds hh
    WHERE hh.ref = $1
      AND EXISTS (SELECT 1 FROM ledger_accounts sa WHERE sa.id = hh.account AND sa.tenant = $4)
    FOR UPDATE OF hh
), existing AS (
    SELECT seq, dst_account, amount::text AS amount, currency, created_at
    FROM ledger_postings WHERE ref = $1
), amt AS (
    SELECT COALESCE($3::numeric, h.amount) AS v FROM h
), dst AS (
    SELECT id, currency, floor FROM ledger_accounts WHERE id = $2 AND tenant = $4
), ok AS (
    SELECT 1 AS yes FROM h, amt, dst
    WHERE h.status = 'open'
      AND amt.v > 0 AND amt.v <= h.amount
      AND dst.currency = h.currency
      AND h.account <> dst.id
), locked AS (
    SELECT a.id FROM ledger_accounts a, h
    WHERE a.id IN (h.account, $2) AND a.floor IS NOT NULL
      AND EXISTS (SELECT 1 FROM ok)
    ORDER BY a.id
    FOR UPDATE OF a
), upd AS (
    UPDATE ledger_accounts a
    SET balance = a.balance + CASE WHEN a.id = (SELECT account FROM h)
                                   THEN -(SELECT v FROM amt) ELSE (SELECT v FROM amt) END,
        held    = a.held    - CASE WHEN a.id = (SELECT account FROM h)
                                   THEN (SELECT amount FROM h) ELSE 0 END
    FROM locked l
    WHERE a.id = l.id
    RETURNING a.id
), updhold AS (
    UPDATE ledger_holds SET status = 'settled', resolved_at = $5
    WHERE ref = $1 AND EXISTS (SELECT 1 FROM ok)
    RETURNING ref
), ins AS (
    INSERT INTO ledger_postings (ref, group_ref, src_account, dst_account, amount, currency, adjusts, created_at)
    SELECT $1, '', h.account, $2, amt.v, h.currency, '', $5 FROM h, amt
    WHERE EXISTS (SELECT 1 FROM ok)
    RETURNING seq
)
SELECT
    EXISTS (SELECT 1 FROM h),
    (SELECT account FROM h), (SELECT amount_text FROM h), (SELECT currency FROM h), (SELECT status FROM h),
    EXISTS (SELECT 1 FROM dst),
    (SELECT seq FROM ins),
    e.seq, e.dst_account, e.amount, e.currency, e.created_at
FROM (SELECT 1) AS one
LEFT JOIN existing e ON true`

// Settle resolves an open hold by writing its single real posting: the held
// amount releases in full, the settle amount (full by default, or
// SettleAmount for partial capture) moves from the hold's account to dst,
// and the posting carries the hold's ref. Settling never fails on funds —
// available can only rise — and ignores ExpiresAt: expiry is sweep policy,
// not a settle gate.
//
// Settle is idempotent: settling a settled hold returns the original posting
// (ErrRefConflict if dst or amount differ — a partial settle must replay with
// the same SettleAmount, or the default full amount will not match the
// original posting). A voided hold returns ErrAlreadyVoided.
func (l *Ledger) Settle(ctx context.Context, tx pgx.Tx, ref string, dst id.UUID, opts ...SettleOption) (Posting, error) {
	if ref == "" {
		return Posting{}, fmt.Errorf("%w: hold ref is required", ErrInvalidRef)
	}
	var cfg settleConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	var amountParam *string
	if cfg.amount.Valid {
		if !cfg.amount.Money.IsPositive() {
			return Posting{}, fmt.Errorf("%w: settle amount must be positive, got %s", ErrInvalidAmount, cfg.amount.Money)
		}
		amountParam = new(cfg.amount.Money.Amount().String())
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Posting{}, err
	}

	now := l.clk.Now().UTC()
	var (
		found, dstFound                      bool
		holdAccount                          *id.UUID
		holdAmount, holdCurrency, holdStatus *string
		newSeq                               *int64
		ex                                   existingSettle
	)
	err = tx.QueryRow(ctx, settleSQL, ref, dst, amountParam, tenant, now).Scan(
		&found, &holdAccount, &holdAmount, &holdCurrency, &holdStatus,
		&dstFound, &newSeq,
		&ex.seq, &ex.dst, &ex.amount, &ex.currency, &ex.createdAt)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			// The hold ref collides with a posting ref written by Post:
			// settle postings carry the hold's ref, so the two share one
			// namespace.
			return Posting{}, fmt.Errorf("%w: ref %q already names a posting", ErrRefConflict, ref)
		}
		return Posting{}, fmt.Errorf("ledger: settle %q: %w", ref, err)
	}

	if !found || holdAccount == nil || holdAmount == nil || holdCurrency == nil || holdStatus == nil {
		return Posting{}, fmt.Errorf("%w: ref %q", ErrHoldNotFound, ref)
	}
	held, err := parseMoney(*holdAmount, *holdCurrency)
	if err != nil {
		return Posting{}, err
	}
	settled := held
	if cfg.amount.Valid {
		settled = cfg.amount.Money
	}
	switch HoldStatus(*holdStatus) {
	case HoldVoided:
		return Posting{}, fmt.Errorf("%w: ref %q", ErrAlreadyVoided, ref)
	case HoldSettled:
		if ex.seq == nil {
			// The hold settled in a transaction that committed after this
			// statement's snapshot was taken: the row lock revealed the new
			// status, but the settle posting is not yet visible here.
			return Posting{}, fmt.Errorf("%w: ref %q", ErrRefRace, ref)
		}
		orig := Posting{
			Seq: *ex.seq, Ref: ref, Src: *holdAccount, Dst: *ex.dst, CreatedAt: *ex.createdAt,
		}
		if orig.Amount, err = parseMoney(*ex.amount, *ex.currency); err != nil {
			return Posting{}, err
		}
		if eq, err := orig.Amount.Equal(settled); err != nil || !eq || orig.Dst != dst {
			return Posting{}, fmt.Errorf("%w: ref %q", ErrRefConflict, ref)
		}
		return orig, nil
	}

	// The hold is open; classify why the settle did not apply.
	if newSeq == nil {
		switch {
		case cfg.amount.Valid && cfg.amount.Money.Currency().Code != *holdCurrency:
			return Posting{}, fmt.Errorf("%w: settle is %s, hold is %s",
				ErrCurrencyMismatch, cfg.amount.Money.Currency().Code, *holdCurrency)
		case cfg.amount.Valid && mustExceeds(settled, held):
			return Posting{}, fmt.Errorf("%w: settle %s, held %s", ErrExceedsHold, settled, held)
		case !dstFound:
			return Posting{}, fmt.Errorf("%w: dst %s", ErrAccountNotFound, dst)
		case *holdAccount == dst:
			return Posting{}, fmt.Errorf("%w: %s", ErrSameAccount, dst)
		default:
			return Posting{}, fmt.Errorf("%w: dst %s is not %s", ErrCurrencyMismatch, dst, *holdCurrency)
		}
	}
	return Posting{Seq: *newSeq, Ref: ref, Src: *holdAccount, Dst: dst, Amount: settled, CreatedAt: now}, nil
}

// existingSettle scans the nullable replay columns of settleSQL.
type existingSettle struct {
	seq       *int64
	dst       *id.UUID
	amount    *string
	currency  *string
	createdAt *time.Time
}

// mustExceeds reports settled > held; a currency mismatch was ruled out by
// the caller, so the comparison cannot fail.
func mustExceeds(settled, held money.Money) bool {
	gt, err := settled.GreaterThan(held)
	return err == nil && gt
}

// voidSQL releases an open hold in one statement: the hold row locks first,
// the floored account's held drops by the hold amount, the hold flips to
// voided. No posting is written — a voided hold leaves no money history.
const voidSQL = `
WITH h AS (
    SELECT ref, account, amount, status FROM ledger_holds hh
    WHERE hh.ref = $1
      AND EXISTS (SELECT 1 FROM ledger_accounts sa WHERE sa.id = hh.account AND sa.tenant = $2)
    FOR UPDATE OF hh
), ok AS (
    SELECT 1 AS yes FROM h WHERE h.status = 'open'
), upd AS (
    UPDATE ledger_accounts a
    SET held = a.held - (SELECT amount FROM h)
    WHERE a.id = (SELECT account FROM h) AND a.floor IS NOT NULL
      AND EXISTS (SELECT 1 FROM ok)
    RETURNING a.id
), updhold AS (
    UPDATE ledger_holds SET status = 'voided', resolved_at = $3
    WHERE ref = $1 AND EXISTS (SELECT 1 FROM ok)
    RETURNING ref
)
SELECT EXISTS (SELECT 1 FROM h), (SELECT status FROM h)`

// Void releases an open hold: held drops by the hold amount, no posting is
// written. Voiding a voided hold is an idempotent no-op; voiding a settled
// hold returns ErrAlreadySettled — money already moved, and corrections are
// forward postings with an Adjusts back-reference, never rewrites. Void
// ignores ExpiresAt: sweeping expired holds is the consumer's policy.
func (l *Ledger) Void(ctx context.Context, tx pgx.Tx, ref string) error {
	if ref == "" {
		return fmt.Errorf("%w: hold ref is required", ErrInvalidRef)
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return err
	}
	var (
		found  bool
		status *string
	)
	err = tx.QueryRow(ctx, voidSQL, ref, tenant, l.clk.Now().UTC()).Scan(&found, &status)
	if err != nil {
		return fmt.Errorf("ledger: void %q: %w", ref, err)
	}
	if !found || status == nil {
		return fmt.Errorf("%w: ref %q", ErrHoldNotFound, ref)
	}
	if HoldStatus(*status) == HoldSettled {
		return fmt.Errorf("%w: ref %q", ErrAlreadySettled, ref)
	}
	return nil
}

const holdSelect = `
SELECT ref, account, amount::text, currency, status, expires_at, created_at, resolved_at
FROM ledger_holds h `

// holdTenantGuard scopes hold reads to the resolved tenant.
const holdTenantGuard = `EXISTS (SELECT 1 FROM ledger_accounts ta WHERE ta.id = h.account AND ta.tenant = `

// HoldByRef fetches one hold by ref.
func (l *Ledger) HoldByRef(ctx context.Context, db DB, ref string) (Hold, error) {
	if ref == "" {
		return Hold{}, fmt.Errorf("%w: ref is required", ErrInvalidRef)
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Hold{}, err
	}
	row := db.QueryRow(ctx, holdSelect+`WHERE ref = $1 AND `+holdTenantGuard+`$2)`, ref, tenant)
	hold, err := scanHold(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Hold{}, fmt.Errorf("%w: ref %q", ErrHoldNotFound, ref)
		}
		return Hold{}, err
	}
	return hold, nil
}

// ExpiredHolds lists open holds whose ExpiresAt has passed, oldest expiry
// first, capped at limit. The ledger never acts on expiry itself: the
// consumer sweeps and applies its own policy — a bet hold voids, a CPA hold
// settles — through the same Void/Settle calls.
func (l *Ledger) ExpiredHolds(ctx context.Context, db DB, limit int) ([]Hold, error) {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx,
		holdSelect+`WHERE status = 'open' AND expires_at IS NOT NULL AND expires_at <= $1
			AND `+holdTenantGuard+`$2) ORDER BY expires_at LIMIT $3`,
		l.clk.Now().UTC(), tenant, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: expired holds: %w", err)
	}
	defer rows.Close()
	var out []Hold
	for rows.Next() {
		hold, err := scanHold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, hold)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: expired holds: %w", err)
	}
	return out, nil
}

// scanHold reads one holdSelect row.
func scanHold(row pgx.Row) (Hold, error) {
	var (
		hold                 Hold
		amount, code, status string
		expires, resolved    *time.Time
	)
	err := row.Scan(&hold.Ref, &hold.Account, &amount, &code, &status, &expires, &hold.CreatedAt, &resolved)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Hold{}, err
		}
		return Hold{}, fmt.Errorf("ledger: scan hold: %w", err)
	}
	hold.Status = HoldStatus(status)
	if hold.Amount, err = parseMoney(amount, code); err != nil {
		return Hold{}, err
	}
	if expires != nil {
		hold.ExpiresAt = *expires
	}
	if resolved != nil {
		hold.ResolvedAt = *resolved
	}
	return hold, nil
}

// existingHold scans the nullable replay columns of holdSQL.
type existingHold struct {
	account    *id.UUID
	amount     *string
	currency   *string
	status     *string
	expiresAt  *time.Time
	createdAt  *time.Time
	resolvedAt *time.Time
}

// hold materializes the replayed row.
func (e existingHold) hold(ref string) (Hold, error) {
	amt, err := parseMoney(*e.amount, *e.currency)
	if err != nil {
		return Hold{}, err
	}
	h := Hold{Ref: ref, Account: *e.account, Amount: amt, Status: HoldStatus(*e.status), CreatedAt: *e.createdAt}
	if e.expiresAt != nil {
		h.ExpiresAt = *e.expiresAt
	}
	if e.resolvedAt != nil {
		h.ResolvedAt = *e.resolvedAt
	}
	return h, nil
}
