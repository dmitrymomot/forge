//go:build integration

package ledger_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/finance/ledger"
)

// Wallets are minted deep enough that floor checks never trip during a run.

// BenchmarkPost is the bet-hot path: one floored wallet debit, one floor-free
// house credit, one posting insert, each in its own transaction.
func BenchmarkPost(b *testing.B) {
	l := ledger.New()
	pool := openPool(b)
	wallet, house := fixture(b, l, pool, 1_000_000_000_00)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := l.Post(ctx, tx, ledger.Posting{
				Ref: fmt.Sprintf("bp-%s-%d", wallet.Owner, i), Src: wallet.ID, Dst: house.ID, Amount: eur(1_00),
			})
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPost_SharedHouse measures the no-hotspot claim: many wallets
// crediting one floor-free house concurrently — the house row is never
// locked, so throughput scales with the wallets, not the house.
func BenchmarkPost_SharedHouse(b *testing.B) {
	l := ledger.New()
	pool := openPool(b)
	_, house := fixture(b, l, pool, 0)
	ctx := context.Background()

	var walletSeq atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		wallet, _ := fixture(b, l, pool, 1_000_000_000_00)
		n := walletSeq.Add(1)
		i := 0
		for pb.Next() {
			i++
			err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
				_, err := l.Post(ctx, tx, ledger.Posting{
					Ref: fmt.Sprintf("bsh-%s-%d-%d", wallet.Owner, n, i), Src: wallet.ID, Dst: house.ID, Amount: eur(1_00),
				})
				return err
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkHoldSettle is the authorize/capture cycle: hold in one
// transaction, settle in another.
func BenchmarkHoldSettle(b *testing.B) {
	l := ledger.New()
	pool := openPool(b)
	wallet, house := fixture(b, l, pool, 1_000_000_000_00)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		r := fmt.Sprintf("bhs-%s-%d", wallet.Owner, i)
		err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := l.Hold(ctx, tx, ledger.Hold{Ref: r, Account: wallet.ID, Amount: eur(1_00)})
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
		err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := l.Settle(ctx, tx, r, house.ID)
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBalance_Floored reads a materialized balance.
func BenchmarkBalance_Floored(b *testing.B) {
	l := ledger.New()
	pool := openPool(b)
	wallet, _ := fixture(b, l, pool, 100_00)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		if _, err := l.Balance(ctx, pool, wallet.ID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBalance_FloorFree reads a snapshot-derived balance with postings
// accumulated past the snapshot horizon.
func BenchmarkBalance_FloorFree(b *testing.B) {
	l := ledger.New()
	pool := openPool(b)
	wallet, house := fixture(b, l, pool, 1_000_000_00)
	ctx := context.Background()
	for i := range 500 {
		err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := l.Post(ctx, tx, ledger.Posting{
				Ref: fmt.Sprintf("bbf-%s-%d", wallet.Owner, i), Src: wallet.ID, Dst: house.ID, Amount: eur(1_00),
			})
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
		if i == 250 {
			if err := l.Snapshot(ctx, pool, house.ID); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ResetTimer()
	for b.Loop() {
		if _, err := l.Balance(ctx, pool, house.ID); err != nil {
			b.Fatal(err)
		}
	}
}
