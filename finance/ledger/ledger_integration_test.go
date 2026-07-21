//go:build integration

package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/finance/ledger"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var (
	poolOnce   sync.Once
	sharedPool *pgxpool.Pool
)

// openPool connects to the suite's Postgres and applies the ledger migration
// once. Tests share tables and isolate by unique owners and refs — never by
// TRUNCATE, which would race parallel tests.
func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	poolOnce.Do(func() {
		cfg := postgres.DefaultConfig()
		cfg.URL = pgtest.DSN(tb)
		pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
		require.NoError(tb, err)
		db := stdlib.OpenDBFromPool(pool)
		defer func() { _ = db.Close() }()
		require.NoError(tb, migration.New(ledger.Migrations, migration.WithTable("forge_ledger_schema")).Up(context.Background(), db))
		sharedPool = pool
	})
	return sharedPool
}

// inTx runs fn inside a committed transaction.
func inTx(tb testing.TB, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) {
	tb.Helper()
	require.NoError(tb, postgres.WithTx(context.Background(), pool, fn))
}

func eur(minor int64) money.Money { return money.FromMinor(minor, money.EUR) }

// fixture creates a fresh floored wallet and floor-free house under a unique
// owner, minting the wallet balance from the house.
func fixture(tb testing.TB, l *ledger.Ledger, pool *pgxpool.Pool, balance int64) (wallet, house ledger.Account) {
	tb.Helper()
	ctx := context.Background()
	owner := "own-" + id.NewULID().String()
	inTx(tb, pool, func(tx pgx.Tx) error {
		var err error
		wallet, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "wallet", Currency: money.EUR})
		require.NoError(tb, err)
		house, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "house", Currency: money.EUR},
			ledger.WithoutFloor())
		require.NoError(tb, err)
		if balance > 0 {
			_, err = l.Post(ctx, tx, ledger.Posting{
				Ref: "mint-" + id.NewULID().String(), Src: house.ID, Dst: wallet.ID, Amount: eur(balance),
			})
			require.NoError(tb, err)
		}
		return nil
	})
	return wallet, house
}

func requireBalance(tb testing.TB, l *ledger.Ledger, pool *pgxpool.Pool, acct id.UUID, balance, held int64) {
	tb.Helper()
	b, err := l.Balance(context.Background(), pool, acct)
	require.NoError(tb, err)
	assertMoneyEq(tb, eur(balance), b.Balance, "balance")
	assertMoneyEq(tb, eur(held), b.Held, "held")
	assertMoneyEq(tb, eur(balance-held), b.Available, "available")
}

func assertMoneyEq(tb testing.TB, want, got money.Money, what string) {
	tb.Helper()
	eq, err := want.Equal(got)
	require.NoError(tb, err, what)
	require.True(tb, eq, "%s: want %s, got %s", what, want, got)
}

func ref(prefix string) string { return prefix + "-" + id.NewULID().String() }

func TestIntegration_EnsureAccount(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	owner := "own-" + id.NewULID().String()
	key := ledger.AccountKey{Owner: owner, Purpose: "wallet", Currency: money.EUR}

	var first, replay, other ledger.Account
	inTx(t, pool, func(tx pgx.Tx) error {
		var err error
		first, err = l.EnsureAccount(ctx, tx, key)
		require.NoError(t, err)
		// Replay with a different floor option: the stored floor wins.
		replay, err = l.EnsureAccount(ctx, tx, key, ledger.WithFloor(eur(-10_00)))
		require.NoError(t, err)
		other, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "bonus", Currency: money.EUR})
		require.NoError(t, err)
		return nil
	})
	assert.Equal(t, first.ID, replay.ID)
	require.True(t, replay.Floor.Valid)
	assertMoneyEq(t, eur(0), replay.Floor.Money, "replayed floor")
	assert.NotEqual(t, first.ID, other.ID)

	got, err := l.AccountByKey(ctx, pool, key)
	require.NoError(t, err)
	assert.Equal(t, first.ID, got.ID)
	assert.Equal(t, owner, got.Owner)
	require.True(t, got.Floor.Valid)

	byID, err := l.AccountByID(ctx, pool, first.ID)
	require.NoError(t, err)
	assert.Equal(t, key.Owner, byID.Owner)

	_, err = l.AccountByID(ctx, pool, id.NewUUID())
	require.ErrorIs(t, err, ledger.ErrAccountNotFound)

	// Floor-free account reports an invalid Floor.
	var free ledger.Account
	inTx(t, pool, func(tx pgx.Tx) error {
		var err error
		free, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "house", Currency: money.EUR},
			ledger.WithoutFloor())
		return err
	})
	assert.False(t, free.Floor.Valid)
}

func TestIntegration_Post(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, house := fixture(t, l, pool, 100_00)

	requireBalance(t, l, pool, wallet.ID, 100_00, 0)

	betRef := ref("bet")
	var posted ledger.Posting
	inTx(t, pool, func(tx pgx.Tx) error {
		var err error
		posted, err = l.Post(ctx, tx, ledger.Posting{Ref: betRef, Src: wallet.ID, Dst: house.ID, Amount: eur(30_00)})
		require.NoError(t, err)
		return nil
	})
	assert.Positive(t, posted.Seq)
	requireBalance(t, l, pool, wallet.ID, 70_00, 0)

	t.Run("replay returns original", func(t *testing.T) {
		inTx(t, pool, func(tx pgx.Tx) error {
			again, err := l.Post(ctx, tx, ledger.Posting{Ref: betRef, Src: wallet.ID, Dst: house.ID, Amount: eur(30_00)})
			require.NoError(t, err)
			assert.Equal(t, posted.Seq, again.Seq)
			assertMoneyEq(t, eur(30_00), again.Amount, "replayed amount")
			return nil
		})
		requireBalance(t, l, pool, wallet.ID, 70_00, 0) // no double spend
	})

	t.Run("replay with different params", func(t *testing.T) {
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Post(ctx, tx, ledger.Posting{Ref: betRef, Src: wallet.ID, Dst: house.ID, Amount: eur(31_00)})
			require.ErrorIs(t, err, ledger.ErrRefConflict)
			return nil
		})
	})

	t.Run("insufficient funds at the boundary", func(t *testing.T) {
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Post(ctx, tx, ledger.Posting{Ref: ref("over"), Src: wallet.ID, Dst: house.ID, Amount: eur(70_01)})
			require.ErrorIs(t, err, ledger.ErrInsufficientFunds)
			// The failed post modified nothing and the tx stays usable.
			_, err = l.Post(ctx, tx, ledger.Posting{Ref: ref("exact"), Src: wallet.ID, Dst: house.ID, Amount: eur(70_00)})
			require.NoError(t, err)
			return nil
		})
		requireBalance(t, l, pool, wallet.ID, 0, 0)
	})

	t.Run("unknown accounts", func(t *testing.T) {
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Post(ctx, tx, ledger.Posting{Ref: ref("ghost"), Src: id.NewUUID(), Dst: house.ID, Amount: eur(1)})
			require.ErrorIs(t, err, ledger.ErrAccountNotFound)
			_, err = l.Post(ctx, tx, ledger.Posting{Ref: ref("ghost"), Src: house.ID, Dst: id.NewUUID(), Amount: eur(1)})
			require.ErrorIs(t, err, ledger.ErrAccountNotFound)
			return nil
		})
	})

	t.Run("currency mismatch", func(t *testing.T) {
		var usd ledger.Account
		inTx(t, pool, func(tx pgx.Tx) error {
			var err error
			usd, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: wallet.Owner, Purpose: "wallet", Currency: money.USD})
			return err
		})
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Post(ctx, tx, ledger.Posting{Ref: ref("fx"), Src: house.ID, Dst: usd.ID, Amount: eur(1_00)})
			require.ErrorIs(t, err, ledger.ErrCurrencyMismatch)
			_, err = l.Post(ctx, tx, ledger.Posting{Ref: ref("fx"), Src: house.ID, Dst: usd.ID, Amount: money.FromMinor(1_00, money.USD)})
			require.ErrorIs(t, err, ledger.ErrCurrencyMismatch)
			return nil
		})
	})
}

func TestIntegration_GroupAndAdjusts(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, house := fixture(t, l, pool, 0)

	group := ref("deposit")
	inTx(t, pool, func(tx pgx.Tx) error {
		// deposit 100 = wallet +97, fee +3: two pairwise postings, one group.
		_, err := l.Post(ctx, tx, ledger.Posting{Ref: group + "-net", GroupRef: group, Src: house.ID, Dst: wallet.ID, Amount: eur(97_00)})
		require.NoError(t, err)
		fee, err := l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: wallet.Owner, Purpose: "fees", Currency: money.EUR},
			ledger.WithoutFloor())
		require.NoError(t, err)
		_, err = l.Post(ctx, tx, ledger.Posting{Ref: group + "-fee", GroupRef: group, Src: house.ID, Dst: fee.ID, Amount: eur(3_00)})
		require.NoError(t, err)
		return nil
	})

	got, err := l.PostingsByGroup(ctx, pool, group)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Less(t, got[0].Seq, got[1].Seq)

	// A forward correction back-references the posting it adjusts.
	adjRef := ref("adjust")
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := l.Post(ctx, tx, ledger.Posting{
			Ref: adjRef, Src: wallet.ID, Dst: house.ID, Amount: eur(2_00), Adjusts: group + "-net",
		})
		require.NoError(t, err)
		return nil
	})
	adj, err := l.PostingByRef(ctx, pool, adjRef)
	require.NoError(t, err)
	assert.Equal(t, group+"-net", adj.Adjusts)

	_, err = l.PostingByRef(ctx, pool, ref("missing"))
	require.ErrorIs(t, err, ledger.ErrPostingNotFound)

	// Statement listing: newest first, keyset pagination.
	all, err := l.Postings(ctx, pool, wallet.ID, 0, 10)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Greater(t, all[0].Seq, all[1].Seq)
	next, err := l.Postings(ctx, pool, wallet.ID, all[0].Seq, 10)
	require.NoError(t, err)
	require.Len(t, next, 1)
	assert.Equal(t, all[1].Seq, next[0].Seq)
}

func TestIntegration_HoldSettleVoid(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()

	t.Run("full settle", func(t *testing.T) {
		t.Parallel()
		wallet, house := fixture(t, l, pool, 50_00)
		betRef := ref("bet")
		inTx(t, pool, func(tx pgx.Tx) error {
			h, err := l.Hold(ctx, tx, ledger.Hold{Ref: betRef, Account: wallet.ID, Amount: eur(20_00)})
			require.NoError(t, err)
			assert.Equal(t, ledger.HoldOpen, h.Status)
			return nil
		})
		requireBalance(t, l, pool, wallet.ID, 50_00, 20_00)

		var posted ledger.Posting
		inTx(t, pool, func(tx pgx.Tx) error {
			var err error
			posted, err = l.Settle(ctx, tx, betRef, house.ID)
			require.NoError(t, err)
			return nil
		})
		assert.Equal(t, betRef, posted.Ref)
		assertMoneyEq(t, eur(20_00), posted.Amount, "settled amount")
		requireBalance(t, l, pool, wallet.ID, 30_00, 0)

		h, err := l.HoldByRef(ctx, pool, betRef)
		require.NoError(t, err)
		assert.Equal(t, ledger.HoldSettled, h.Status)
		assert.False(t, h.ResolvedAt.IsZero())

		// Settle replay returns the original posting; different dst conflicts.
		inTx(t, pool, func(tx pgx.Tx) error {
			again, err := l.Settle(ctx, tx, betRef, house.ID)
			require.NoError(t, err)
			assert.Equal(t, posted.Seq, again.Seq)
			_, err = l.Settle(ctx, tx, betRef, wallet.ID)
			require.ErrorIs(t, err, ledger.ErrRefConflict)
			return nil
		})

		// Void after settle refuses.
		inTx(t, pool, func(tx pgx.Tx) error {
			require.ErrorIs(t, l.Void(ctx, tx, betRef), ledger.ErrAlreadySettled)
			return nil
		})
	})

	t.Run("partial settle releases the remainder", func(t *testing.T) {
		t.Parallel()
		wallet, house := fixture(t, l, pool, 100_00)
		authRef := ref("auth")
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: authRef, Account: wallet.ID, Amount: eur(100_00)})
			require.NoError(t, err)
			return nil
		})
		inTx(t, pool, func(tx pgx.Tx) error {
			p, err := l.Settle(ctx, tx, authRef, house.ID, ledger.SettleAmount(eur(97_00)))
			require.NoError(t, err)
			assertMoneyEq(t, eur(97_00), p.Amount, "partial settle")
			return nil
		})
		requireBalance(t, l, pool, wallet.ID, 3_00, 0)
	})

	t.Run("settle amount exceeds hold", func(t *testing.T) {
		t.Parallel()
		wallet, house := fixture(t, l, pool, 10_00)
		r := ref("small")
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(5_00)})
			require.NoError(t, err)
			_, err = l.Settle(ctx, tx, r, house.ID, ledger.SettleAmount(eur(6_00)))
			require.ErrorIs(t, err, ledger.ErrExceedsHold)
			return nil
		})
	})

	t.Run("void releases", func(t *testing.T) {
		t.Parallel()
		wallet, house := fixture(t, l, pool, 40_00)
		r := ref("void")
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(15_00)})
			require.NoError(t, err)
			return nil
		})
		requireBalance(t, l, pool, wallet.ID, 40_00, 15_00)
		inTx(t, pool, func(tx pgx.Tx) error {
			require.NoError(t, l.Void(ctx, tx, r))
			return nil
		})
		requireBalance(t, l, pool, wallet.ID, 40_00, 0)

		// Void is idempotent; settle after void refuses.
		inTx(t, pool, func(tx pgx.Tx) error {
			require.NoError(t, l.Void(ctx, tx, r))
			_, err := l.Settle(ctx, tx, r, house.ID)
			require.ErrorIs(t, err, ledger.ErrAlreadyVoided)
			return nil
		})
		// No posting was ever written for a voided hold.
		_, err := l.PostingByRef(ctx, pool, r)
		require.ErrorIs(t, err, ledger.ErrPostingNotFound)
	})

	t.Run("hold beyond available", func(t *testing.T) {
		t.Parallel()
		wallet, _ := fixture(t, l, pool, 30_00)
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: ref("h1"), Account: wallet.ID, Amount: eur(25_00)})
			require.NoError(t, err)
			_, err = l.Hold(ctx, tx, ledger.Hold{Ref: ref("h2"), Account: wallet.ID, Amount: eur(6_00)})
			require.ErrorIs(t, err, ledger.ErrInsufficientFunds)
			return nil
		})
	})

	t.Run("hold replay and conflicts", func(t *testing.T) {
		t.Parallel()
		wallet, _ := fixture(t, l, pool, 30_00)
		r := ref("replay")
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(10_00)})
			require.NoError(t, err)
			return nil
		})
		inTx(t, pool, func(tx pgx.Tx) error {
			again, err := l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(10_00)})
			require.NoError(t, err)
			assert.Equal(t, ledger.HoldOpen, again.Status)
			_, err = l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(11_00)})
			require.ErrorIs(t, err, ledger.ErrRefConflict)
			return nil
		})
		requireBalance(t, l, pool, wallet.ID, 30_00, 10_00) // replay reserved once

		// Nanosecond-precision expiry survives the timestamptz microsecond
		// truncation: an identical replay must not read as a conflict.
		nano := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond).Add(789 * time.Nanosecond)
		nr := ref("nano")
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: nr, Account: wallet.ID, Amount: eur(1_00), ExpiresAt: nano})
			require.NoError(t, err)
			return nil
		})
		inTx(t, pool, func(tx pgx.Tx) error {
			again, err := l.Hold(ctx, tx, ledger.Hold{Ref: nr, Account: wallet.ID, Amount: eur(1_00), ExpiresAt: nano})
			require.NoError(t, err, "same nanosecond expiry must replay cleanly")
			assert.Equal(t, ledger.HoldOpen, again.Status)
			return nil
		})
	})

	t.Run("hold on floor-free account", func(t *testing.T) {
		t.Parallel()
		_, house := fixture(t, l, pool, 0)
		r := ref("housed")
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: house.ID, Amount: eur(500_00)})
			require.NoError(t, err)
			return nil
		})
		// Floor-free held derives from open hold rows.
		b, err := l.Balance(ctx, pool, house.ID)
		require.NoError(t, err)
		assertMoneyEq(t, eur(500_00), b.Held, "derived held")
		inTx(t, pool, func(tx pgx.Tx) error { return l.Void(ctx, tx, r) })
		b, err = l.Balance(ctx, pool, house.ID)
		require.NoError(t, err)
		assertMoneyEq(t, eur(0), b.Held, "held after void")
	})

	t.Run("hold ref colliding with a posting ref", func(t *testing.T) {
		t.Parallel()
		wallet, house := fixture(t, l, pool, 20_00)
		r := ref("shared")
		inTx(t, pool, func(tx pgx.Tx) error {
			_, err := l.Post(ctx, tx, ledger.Posting{Ref: r, Src: wallet.ID, Dst: house.ID, Amount: eur(1_00)})
			require.NoError(t, err)
			_, err = l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(1_00)})
			require.NoError(t, err) // holds table admits it...
			return nil
		})
		// ...but settling would collide with the posting namespace.
		err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := l.Settle(ctx, tx, r, house.ID)
			return err
		})
		require.ErrorIs(t, err, ledger.ErrRefConflict)
	})
}

func TestIntegration_ExpiredHolds(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, _ := fixture(t, l, pool, 100_00)

	past := time.Now().UTC().Add(-time.Minute)
	future := time.Now().UTC().Add(time.Hour)
	expiredRef, liveRef := ref("expired"), ref("live")
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := l.Hold(ctx, tx, ledger.Hold{Ref: expiredRef, Account: wallet.ID, Amount: eur(1_00), ExpiresAt: past})
		require.NoError(t, err)
		_, err = l.Hold(ctx, tx, ledger.Hold{Ref: liveRef, Account: wallet.ID, Amount: eur(1_00), ExpiresAt: future})
		require.NoError(t, err)
		_, err = l.Hold(ctx, tx, ledger.Hold{Ref: ref("never"), Account: wallet.ID, Amount: eur(1_00)})
		require.NoError(t, err)
		return nil
	})

	holds, err := l.ExpiredHolds(ctx, pool, 100)
	require.NoError(t, err)
	refs := make(map[string]bool, len(holds))
	for _, h := range holds {
		refs[h.Ref] = true
	}
	assert.True(t, refs[expiredRef])
	assert.False(t, refs[liveRef])

	// The sweep applies consumer policy through the same Void call.
	inTx(t, pool, func(tx pgx.Tx) error { return l.Void(ctx, tx, expiredRef) })
	holds, err = l.ExpiredHolds(ctx, pool, 100)
	require.NoError(t, err)
	for _, h := range holds {
		assert.NotEqual(t, expiredRef, h.Ref)
	}
}

func TestIntegration_FloorSemantics(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	owner := "own-" + id.NewULID().String()

	// Overdraft floor: available may go down to the negative floor.
	var od, sink ledger.Account
	inTx(t, pool, func(tx pgx.Tx) error {
		var err error
		od, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "overdraft", Currency: money.EUR},
			ledger.WithFloor(eur(-50_00)))
		require.NoError(t, err)
		sink, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "sink", Currency: money.EUR},
			ledger.WithoutFloor())
		return err
	})
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := l.Post(ctx, tx, ledger.Posting{Ref: ref("od"), Src: od.ID, Dst: sink.ID, Amount: eur(50_00)})
		require.NoError(t, err)
		_, err = l.Post(ctx, tx, ledger.Posting{Ref: ref("od2"), Src: od.ID, Dst: sink.ID, Amount: eur(0_01)})
		require.ErrorIs(t, err, ledger.ErrInsufficientFunds)
		return nil
	})
	requireBalance(t, l, pool, od.ID, -50_00, 0)

	// Floor-free src never runs a funds check.
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := l.Post(ctx, tx, ledger.Posting{Ref: ref("mintmore"), Src: sink.ID, Dst: od.ID, Amount: eur(1_000_00)})
		return err
	})
	requireBalance(t, l, pool, od.ID, 950_00, 0)
}

func TestIntegration_FloorFreeBalanceAndSnapshot(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, house := fixture(t, l, pool, 500_00) // house is at -500 now

	houseBal := func() money.Money {
		b, err := l.Balance(ctx, pool, house.ID)
		require.NoError(t, err)
		return b.Balance
	}
	assertMoneyEq(t, eur(-500_00), houseBal(), "derived without snapshot")

	require.NoError(t, l.Snapshot(ctx, pool, house.ID))
	assertMoneyEq(t, eur(-500_00), houseBal(), "derived from snapshot")

	// Postings after the snapshot are added on top.
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := l.Post(ctx, tx, ledger.Posting{Ref: ref("loss"), Src: wallet.ID, Dst: house.ID, Amount: eur(120_00)})
		return err
	})
	assertMoneyEq(t, eur(-380_00), houseBal(), "snapshot + postings since")

	require.NoError(t, l.Snapshot(ctx, pool, house.ID))
	assertMoneyEq(t, eur(-380_00), houseBal(), "advanced snapshot")

	t.Run("in-flight posting is never lost", func(t *testing.T) {
		// Open a transaction, post to the house, and snapshot BEFORE the
		// commit: the poster's txid is at-or-past the snapshot horizon, so
		// the snapshot excludes it and the balance read adds it after commit
		// — counted exactly once.
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		_, err = l.Post(ctx, tx, ledger.Posting{Ref: ref("inflight"), Src: wallet.ID, Dst: house.ID, Amount: eur(80_00)})
		require.NoError(t, err)

		require.NoError(t, l.Snapshot(ctx, pool, house.ID))
		assertMoneyEq(t, eur(-380_00), houseBal(), "uncommitted posting invisible")

		require.NoError(t, tx.Commit(ctx))
		assertMoneyEq(t, eur(-300_00), houseBal(), "committed posting visible once")

		require.NoError(t, l.Snapshot(ctx, pool, house.ID))
		assertMoneyEq(t, eur(-300_00), houseBal(), "next snapshot absorbs it")
	})

	require.NoError(t, l.Snapshot(ctx, pool, wallet.ID)) // floored accounts may snapshot too

	err := l.Snapshot(ctx, pool, id.NewUUID())
	require.ErrorIs(t, err, ledger.ErrAccountNotFound)
}

func TestIntegration_CheckDrift(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, house := fixture(t, l, pool, 75_00)

	for _, acct := range []id.UUID{wallet.ID, house.ID} {
		d, err := l.CheckDrift(ctx, pool, acct)
		require.NoError(t, err)
		assert.False(t, d.Drifted, "clean account must not drift")
	}

	// Manufacture drift the way a bug or manual edit would: bypass the API.
	_, err := pool.Exec(ctx, `UPDATE ledger_accounts SET balance = balance + 0.01 WHERE id = $1`, wallet.ID)
	require.NoError(t, err)
	d, err := l.CheckDrift(ctx, pool, wallet.ID)
	require.NoError(t, err)
	assert.True(t, d.Drifted)
	assertMoneyEq(t, eur(75_01), d.Reported, "reported")
	assertMoneyEq(t, eur(75_00), d.Computed, "computed")

	// Corrupt a floor-free snapshot: derived view drifts from the recompute.
	require.NoError(t, l.Snapshot(ctx, pool, house.ID))
	_, err = pool.Exec(ctx, `UPDATE ledger_snapshots SET balance = balance - 5 WHERE account = $1`, house.ID)
	require.NoError(t, err)
	d, err = l.CheckDrift(ctx, pool, house.ID)
	require.NoError(t, err)
	assert.True(t, d.Drifted)

	_, err = l.CheckDrift(ctx, pool, id.NewUUID())
	require.ErrorIs(t, err, ledger.ErrAccountNotFound)
}

func TestIntegration_Tenancy(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	ctx := context.Background()

	type tenantKey struct{}
	scoped := ledger.New(ledger.WithScope(func(ctx context.Context) (string, error) {
		s, _ := ctx.Value(tenantKey{}).(string)
		if s == "" {
			return "", errors.New("no tenant")
		}
		return s, nil
	}))
	tenantA := "ten-" + id.NewULID().String()
	tenantB := "ten-" + id.NewULID().String()
	ctxA := context.WithValue(ctx, tenantKey{}, tenantA)
	ctxB := context.WithValue(ctx, tenantKey{}, tenantB)

	owner := "own-" + id.NewULID().String()
	key := ledger.AccountKey{Owner: owner, Purpose: "wallet", Currency: money.EUR}

	var acctA, acctB, houseA ledger.Account
	inTx(t, pool, func(tx pgx.Tx) error {
		var err error
		acctA, err = scoped.EnsureAccount(ctxA, tx, key)
		require.NoError(t, err)
		houseA, err = scoped.EnsureAccount(ctxA, tx, ledger.AccountKey{Owner: owner, Purpose: "house", Currency: money.EUR},
			ledger.WithoutFloor())
		require.NoError(t, err)
		acctB, err = scoped.EnsureAccount(ctxB, tx, key)
		require.NoError(t, err)
		return nil
	})
	// Same key under two tenants: two distinct accounts.
	assert.NotEqual(t, acctA.ID, acctB.ID)
	assert.Equal(t, tenantA, acctA.Tenant)

	// Cross-tenant access fails closed as not-found.
	_, err := scoped.AccountByID(ctxB, pool, acctA.ID)
	require.ErrorIs(t, err, ledger.ErrAccountNotFound)
	_, err = scoped.Balance(ctxB, pool, acctA.ID)
	require.ErrorIs(t, err, ledger.ErrAccountNotFound)
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := scoped.Post(ctxB, tx, ledger.Posting{Ref: ref("steal"), Src: houseA.ID, Dst: acctB.ID, Amount: eur(1_00)})
		require.ErrorIs(t, err, ledger.ErrAccountNotFound)
		return nil
	})

	// Same-tenant flow works and stays invisible to the other tenant.
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := scoped.Post(ctxA, tx, ledger.Posting{Ref: ref("fund"), Src: houseA.ID, Dst: acctA.ID, Amount: eur(10_00)})
		require.NoError(t, err)
		return nil
	})
	accountsB, err := scoped.Accounts(ctxB, pool, id.UUID{}, 1000)
	require.NoError(t, err)
	for _, a := range accountsB {
		assert.NotEqual(t, acctA.ID, a.ID)
	}

	// An unscoped ledger only sees empty-tenant accounts.
	unscoped := ledger.New()
	_, err = unscoped.AccountByID(ctx, pool, acctA.ID)
	require.ErrorIs(t, err, ledger.ErrAccountNotFound)
}

func TestIntegration_ConcurrentSameRef(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, house := fixture(t, l, pool, 100_00)

	const workers = 8
	r := ref("race")
	var applied, raced int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
				_, err := l.Post(ctx, tx, ledger.Posting{Ref: r, Src: wallet.ID, Dst: house.ID, Amount: eur(10_00)})
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				applied++ // first insert or clean replay
			case errors.Is(err, ledger.ErrRefRace):
				raced++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	assert.Positive(t, applied)
	t.Logf("applied=%d raced=%d", applied, raced)
	// Whatever the interleaving, the money moved exactly once.
	requireBalance(t, l, pool, wallet.ID, 90_00, 0)
}

func TestIntegration_ConcurrentFloorEnforcement(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, house := fixture(t, l, pool, 100_00)

	// 20 concurrent spends of 10 against a balance of 100: exactly 10 apply.
	const workers = 20
	var ok, insufficient int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
				_, err := l.Post(ctx, tx, ledger.Posting{
					Ref: fmt.Sprintf("%s-%d", ref("spend"), i), Src: wallet.ID, Dst: house.ID, Amount: eur(10_00),
				})
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ledger.ErrInsufficientFunds):
				insufficient++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 10, ok)
	assert.Equal(t, workers-10, insufficient)
	requireBalance(t, l, pool, wallet.ID, 0, 0)

	d, err := l.CheckDrift(ctx, pool, wallet.ID)
	require.NoError(t, err)
	assert.False(t, d.Drifted)
}

func TestIntegration_ConcurrentHolds(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, _ := fixture(t, l, pool, 50_00)

	const workers = 12
	var ok int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
				_, err := l.Hold(ctx, tx, ledger.Hold{
					Ref: fmt.Sprintf("%s-%d", ref("chold"), i), Account: wallet.ID, Amount: eur(10_00),
				})
				return err
			})
			if err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			} else if !errors.Is(err, ledger.ErrInsufficientFunds) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 5, ok, "held total may never exceed the balance")
	requireBalance(t, l, pool, wallet.ID, 50_00, 50_00)
}

func TestIntegration_ConcurrentSettleVoid(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	wallet, house := fixture(t, l, pool, 30_00)

	r := ref("sv")
	inTx(t, pool, func(tx pgx.Tx) error {
		_, err := l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(30_00)})
		return err
	})

	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0] = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := l.Settle(ctx, tx, r, house.ID)
			return err
		})
	}()
	go func() {
		defer wg.Done()
		results[1] = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			return l.Void(ctx, tx, r)
		})
	}()
	wg.Wait()

	h, err := l.HoldByRef(ctx, pool, r)
	require.NoError(t, err)
	switch h.Status {
	case ledger.HoldSettled:
		require.NoError(t, results[0])
		require.ErrorIs(t, results[1], ledger.ErrAlreadySettled)
		requireBalance(t, l, pool, wallet.ID, 0, 0)
	case ledger.HoldVoided:
		require.NoError(t, results[1])
		require.ErrorIs(t, results[0], ledger.ErrAlreadyVoided)
		requireBalance(t, l, pool, wallet.ID, 30_00, 0)
	default:
		t.Fatalf("hold left unresolved: %s", h.Status)
	}

	d, err := l.CheckDrift(ctx, pool, wallet.ID)
	require.NoError(t, err)
	assert.False(t, d.Drifted)
}

func TestIntegration_CustomCurrency(t *testing.T) {
	t.Parallel()
	pool := openPool(t)
	l := ledger.New()
	ctx := context.Background()
	owner := "own-" + id.NewULID().String()
	coins := money.Currency{Code: "COIN", Symbol: "C", MinorUnits: 0}

	var bank, player ledger.Account
	inTx(t, pool, func(tx pgx.Tx) error {
		var err error
		bank, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "bank", Currency: coins},
			ledger.WithoutFloor())
		require.NoError(t, err)
		player, err = l.EnsureAccount(ctx, tx, ledger.AccountKey{Owner: owner, Purpose: "coins", Currency: coins})
		require.NoError(t, err)
		_, err = l.Post(ctx, tx, ledger.Posting{
			Ref: ref("grant"), Src: bank.ID, Dst: player.ID, Amount: money.FromMinor(250, coins),
		})
		return err
	})
	b, err := l.Balance(ctx, pool, player.ID)
	require.NoError(t, err)
	assert.Equal(t, "COIN", b.Balance.Currency().Code)
	eq, err := b.Balance.Equal(money.FromMinor(250, coins))
	require.NoError(t, err)
	assert.True(t, eq)
}
