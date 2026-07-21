package ledger

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating the ledger_accounts,
// ledger_postings, ledger_holds, and ledger_snapshots tables, rooted so its
// .sql files sit at fsys root. Apply via data/migration under its own version
// table; consumers never write these tables directly.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// DB is the query seam for reads and maintenance ops, satisfied by
// *pgxpool.Pool, *pgx.Conn, and pgx.Tx. Write ops (EnsureAccount, Post, Hold,
// Settle, Void) take pgx.Tx explicitly: their invariants live inside the
// caller's transaction, committing together with the caller's own rows.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Ledger is a stateless handle over the ledger tables: it carries the clock
// and the optional tenant-scope hook, and every method takes the caller's
// connection or transaction. One Ledger is safe for concurrent use.
type Ledger struct {
	clk   clock.Clock
	scope func(ctx context.Context) (string, error)
}

// Option configures New.
type Option func(*Ledger)

// WithClock injects the time source used for created_at/resolved_at stamps
// and expiry comparisons. Defaults to clock.System().
func WithClock(c clock.Clock) Option {
	return func(l *Ledger) { l.clk = c }
}

// WithScope installs the tenant-scope hook. When configured, every operation
// resolves the tenant from the context and touches only accounts created
// under that tenant; a hook returning an error or an empty scope fails the
// operation with ErrScopeMissing (fail closed). Without a hook all accounts
// live under the empty tenant and no scoping applies.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(l *Ledger) { l.scope = fn }
}

// New constructs a Ledger. It performs no I/O; the schema must be applied
// separately via Migrations.
func New(opts ...Option) *Ledger {
	l := &Ledger{clk: clock.System()}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// resolveScope returns the tenant value used in every SQL predicate: "" when
// no hook is configured, otherwise the hook's non-empty result.
func (l *Ledger) resolveScope(ctx context.Context) (string, error) {
	if l.scope == nil {
		return "", nil
	}
	s, err := l.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScopeMissing, err)
	}
	if s == "" {
		return "", ErrScopeMissing
	}
	return s, nil
}

// currencyByCode resolves a stored currency code to ISO-4217 metadata, or a
// bare code-only Currency for custom units (points, coins) that were created
// from consumer-defined money.Currency values.
func currencyByCode(code string) money.Currency {
	if c, ok := money.CurrencyByCode(code); ok {
		return c
	}
	return money.Currency{Code: code, Symbol: code}
}

// parseMoney converts a numeric::text column value plus a currency code into
// money.Money.
func parseMoney(amount, code string) (money.Money, error) {
	d, err := decimal.Parse(amount)
	if err != nil {
		return money.Money{}, fmt.Errorf("ledger: parse stored amount %q: %w", amount, err)
	}
	return money.New(d, currencyByCode(code)), nil
}
