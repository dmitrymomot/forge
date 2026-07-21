package invoice_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/finance/invoice"
)

func benchLines(n int) []invoice.LineItem {
	rates := []decimal.Decimal{
		decimal.MustParse("0"),
		decimal.MustParse("0.07"),
		decimal.MustParse("0.19"),
	}
	lines := make([]invoice.LineItem, n)
	for i := range lines {
		lines[i] = invoice.LineItem{
			Description: fmt.Sprintf("line %d", i),
			Quantity:    decimal.FromInt(int64(1 + i%3)),
			UnitPrice:   money.FromMinor(int64(999+i*137), money.EUR),
			TaxRate:     rates[i%len(rates)],
		}
	}
	return lines
}

func BenchmarkCompute(b *testing.B) {
	for _, n := range []int{1, 10, 100} {
		lines := benchLines(n)
		b.Run(fmt.Sprintf("per_line/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := invoice.Compute(lines, invoice.RoundPerLine); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("per_total/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := invoice.Compute(lines, invoice.RoundPerTotal); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchDraft(lines []invoice.LineItem) *invoice.Invoice {
	inv := invoice.New("INV-2026", money.EUR)
	inv.Issuer = invoice.Party{Name: "Forge GmbH"}
	inv.Recipient = invoice.Party{Name: "ACME Ltd"}
	inv.Lines = lines
	return inv
}

func BenchmarkIssue(b *testing.B) {
	seq, err := invoice.NewSequence(invoice.NewMemorySequenceStore())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	at := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	lines := benchLines(10)
	b.ReportAllocs()
	for b.Loop() {
		if err := benchDraft(lines).Issue(ctx, seq, at); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyPayment(b *testing.B) {
	seq, err := invoice.NewSequence(invoice.NewMemorySequenceStore())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	at := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	lines := benchLines(10)
	proto := benchDraft(lines)
	if err := proto.Issue(ctx, seq, at); err != nil {
		b.Fatal(err)
	}
	p := invoice.Payment{Ref: "ledger:1", Amount: proto.Totals.Total, At: at}
	b.ReportAllocs()
	for b.Loop() {
		inv := *proto
		inv.Payments = nil
		if err := inv.ApplyPayment(ctx, p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSequenceNext(b *testing.B) {
	seq, err := invoice.NewSequence(invoice.NewMemorySequenceStore(), invoice.WithMode(invoice.WithGaps))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := seq.Next(ctx, "INV"); err != nil {
			b.Fatal(err)
		}
	}
}
