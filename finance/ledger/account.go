package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/money"
)

// AccountKey identifies an account within a tenant: who owns it, what it is
// for, and in which currency. All fields are required.
type AccountKey struct {
	Owner    string
	Purpose  string
	Currency money.Currency
}

// Account is a registry row. Floor is valid for floored accounts (available =
// Balance − Held never drops below it) and invalid for floor-free accounts
// (house/mint style: no materialized balance, derived via snapshots).
type Account struct {
	CreatedAt time.Time
	Tenant    string
	Owner     string
	Purpose   string
	Currency  money.Currency
	Floor     money.NullMoney
	ID        id.UUID
}

// accountConfig collects EnsureAccount options.
type accountConfig struct {
	floor     money.Money
	floorFree bool
	floorSet  bool
}

// AccountOption configures EnsureAccount.
type AccountOption func(*accountConfig)

// WithFloor sets the account floor (must match the key currency). The default
// floor is zero in the key currency; a negative floor grants overdraft.
func WithFloor(m money.Money) AccountOption {
	return func(c *accountConfig) { c.floor = m; c.floorSet = true }
}

// WithoutFloor creates a floor-free account: no materialized balance, no
// funds check, no row lock on the hot path — for house/mint accounts that
// absorb or emit money freely.
func WithoutFloor() AccountOption {
	return func(c *accountConfig) { c.floorFree = true }
}

const ensureAccountSQL = `
WITH ins AS (
    INSERT INTO ledger_accounts (id, tenant, owner, purpose, currency, floor, balance, held, created_at)
    VALUES ($1, $2, $3, $4, $5,
            $6::numeric,
            CASE WHEN $6::numeric IS NULL THEN NULL ELSE 0 END,
            CASE WHEN $6::numeric IS NULL THEN NULL ELSE 0 END,
            $7)
    ON CONFLICT (tenant, owner, purpose, currency) DO NOTHING
    RETURNING id, floor::text, created_at
)
SELECT id, floor, created_at FROM ins
UNION ALL
SELECT id, floor::text, created_at FROM ledger_accounts
WHERE tenant = $2 AND owner = $3 AND purpose = $4 AND currency = $5
  AND NOT EXISTS (SELECT 1 FROM ins)`

// EnsureAccount registers an account, idempotently: the first call creates
// it, replays return the existing row unchanged (options on a replay are
// ignored — the stored floor wins). Accounts are never created implicitly by
// a posting; a typo must fail, not mint an account.
//
// By default the account is floored at zero in the key currency.
func (l *Ledger) EnsureAccount(ctx context.Context, tx pgx.Tx, key AccountKey, opts ...AccountOption) (Account, error) {
	if key.Owner == "" || key.Purpose == "" || key.Currency.Code == "" {
		return Account{}, fmt.Errorf("%w: owner, purpose, and currency are required", ErrInvalidKey)
	}
	cfg := accountConfig{floor: money.FromMinor(0, key.Currency)}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.floorFree && cfg.floorSet {
		return Account{}, fmt.Errorf("%w: WithFloor and WithoutFloor are mutually exclusive", ErrInvalidKey)
	}
	if !cfg.floorFree && cfg.floor.Currency().Code != key.Currency.Code {
		return Account{}, fmt.Errorf("%w: floor currency %s does not match account currency %s",
			ErrCurrencyMismatch, cfg.floor.Currency().Code, key.Currency.Code)
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Account{}, err
	}

	var floorParam *string
	if !cfg.floorFree {
		floorParam = new(cfg.floor.Amount().String())
	}
	var (
		acctID    id.UUID
		floorText *string
		createdAt time.Time
	)
	// A concurrent creator makes both arms come back empty: ON CONFLICT DO
	// NOTHING skips the insert, but the row committed after this statement's
	// snapshot, so the fallback select misses it too. The statement does not
	// abort the transaction, and a re-run's fresh snapshot sees the row.
	for attempt := 0; ; attempt++ {
		err = tx.QueryRow(ctx, ensureAccountSQL,
			id.NewUUID(), tenant, key.Owner, key.Purpose, key.Currency.Code, floorParam, l.clk.Now().UTC(),
		).Scan(&acctID, &floorText, &createdAt)
		if err == nil {
			break
		}
		if errors.Is(err, pgx.ErrNoRows) && attempt < 2 {
			continue
		}
		return Account{}, fmt.Errorf("ledger: ensure account: %w", err)
	}
	acct := Account{
		ID: acctID, Tenant: tenant,
		Owner: key.Owner, Purpose: key.Purpose, Currency: key.Currency,
		CreatedAt: createdAt,
	}
	if floorText != nil {
		f, err := parseMoney(*floorText, key.Currency.Code)
		if err != nil {
			return Account{}, err
		}
		acct.Floor = money.NullMoney{Money: f, Valid: true}
	}
	return acct, nil
}

const accountSelect = `
SELECT id, tenant, owner, purpose, currency, floor::text, created_at
FROM ledger_accounts `

// AccountByID fetches an account by id under the resolved tenant.
func (l *Ledger) AccountByID(ctx context.Context, db DB, account id.UUID) (Account, error) {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Account{}, err
	}
	row := db.QueryRow(ctx, accountSelect+`WHERE id = $1 AND tenant = $2`, account, tenant)
	return scanAccount(row)
}

// AccountByKey fetches an account by its registry key under the resolved tenant.
func (l *Ledger) AccountByKey(ctx context.Context, db DB, key AccountKey) (Account, error) {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Account{}, err
	}
	row := db.QueryRow(ctx,
		accountSelect+`WHERE tenant = $1 AND owner = $2 AND purpose = $3 AND currency = $4`,
		tenant, key.Owner, key.Purpose, key.Currency.Code)
	return scanAccount(row)
}

// Accounts lists accounts under the resolved tenant in id order (UUIDv7: time
// order), keyset-paginated: pass the zero UUID to start, the last returned ID
// to continue. Used by drift-check and snapshot sweeps.
func (l *Ledger) Accounts(ctx context.Context, db DB, after id.UUID, limit int) ([]Account, error) {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx,
		accountSelect+`WHERE tenant = $1 AND id > $2 ORDER BY id LIMIT $3`,
		tenant, after, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: list accounts: %w", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		acct, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, acct)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: list accounts: %w", err)
	}
	return out, nil
}

// scanAccount reads one accountSelect row.
func scanAccount(row pgx.Row) (Account, error) {
	var (
		acct      Account
		code      string
		floorText *string
	)
	err := row.Scan(&acct.ID, &acct.Tenant, &acct.Owner, &acct.Purpose, &code, &floorText, &acct.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrAccountNotFound
		}
		return Account{}, fmt.Errorf("ledger: scan account: %w", err)
	}
	acct.Currency = currencyByCode(code)
	if floorText != nil {
		f, err := parseMoney(*floorText, code)
		if err != nil {
			return Account{}, err
		}
		acct.Floor = money.NullMoney{Money: f, Valid: true}
	}
	return acct, nil
}
