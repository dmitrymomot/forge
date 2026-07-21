// Package invoice provides the invoice document model — invariants, not
// rendering: sequential numbering, immutability once issued, penny-perfect
// totals, a payment-matched lifecycle, and credit-note corrections.
//
// An Invoice is a plain, persistable struct; the caller owns storage (the
// fsm philosophy of caller-owned state) and the package operations are the
// only legal way to move it. A draft is freely mutable. Issue validates the
// document, computes and freezes Totals from the line items, draws the
// document number, and moves draft → issued; from then on the document is
// immutable and every correction is a new credit-note document
// back-referencing the original via Corrects (corrections post forward,
// the rule shared with the ledger). Verify re-checks a loaded document's
// totals against its lines to detect drift.
//
// Numbering is per-series through a Sequence over a small SequenceStore
// seam, with two explicit modes because the requirement is jurisdictional:
// Gapless (default) draws only inside Issue — inside the caller's
// persistence transaction when the store rides ctx — and refuses pre-drawn
// or pre-assigned numbers; WithGaps allows pre-drawing via Sequence.Next and
// burns numbers on failed issuance. An in-memory store ships for tests and
// development.
//
// Totals run over the money package with two rounding policies: RoundPerLine
// rounds each line and its tax individually; RoundPerTotal rounds once at
// the document level and allocates the subtotal back across lines
// penny-perfect via money.Allocate. Tax rates are caller-supplied data per
// line — the package never determines them — and group into per-rate
// TaxLines. Payments match by external refs (ledger posting refs) and drive
// issued → partially_paid → paid; MarkOverdue is the caller's sweep;
// Void works only while no payments are recorded. Self-billing (the
// platform issuing on the supplier's behalf — affiliate and agent payouts)
// is the Direction field; a document issued in a non-base currency records
// its FXSnapshot — recorded data, never conversion math.
//
// Lifecycle errors surface the fsm package's sentinels: paying a draft is
// fsm.ErrIllegalTransition, voiding a paid document wraps ErrHasPayments in
// fsm.ErrGuardDenied.
//
// What this is NOT: it does not render documents (HTML is a render recipe,
// PDF stays consumer-side), determine tax rates, speak e-invoicing formats,
// do dunning, or know about subscriptions and pricing — the billing
// anti-scope stands.
//
// # Usage
//
//	store := invoice.NewMemorySequenceStore()
//	seq, err := invoice.NewSequence(store)
//	if err != nil {
//		// nil store or invalid options
//	}
//
//	inv := invoice.New("INV-2026", money.EUR)
//	inv.Issuer = invoice.Party{Name: "Forge GmbH", TaxID: "DE123456789"}
//	inv.Recipient = invoice.Party{Name: "ACME Ltd"}
//	inv.Rounding = invoice.RoundPerTotal
//	inv.Lines = []invoice.LineItem{{
//		Description: "Platform fee, June",
//		Quantity:    decimal.FromInt(1),
//		UnitPrice:   money.FromMinor(49900, money.EUR),
//		TaxRate:     decimal.MustParse("0.19"),
//	}}
//	inv.DueAt = issuedAt.AddDate(0, 0, 14)
//
//	if err := inv.Issue(ctx, seq, issuedAt); err != nil {
//		// validation failed or the transition was illegal
//	}
//	_ = inv.Number             // "INV-2026-000001"
//	_ = inv.Totals.Total       // 593.81 EUR
//
//	err = inv.ApplyPayment(ctx, invoice.Payment{
//		Ref:    "ledger:posting:8f3a", // idempotent by ref
//		Amount: inv.Totals.Total,
//		At:     paidAt,
//	})
//	// inv.Status == invoice.StatusPaid
//
//	note, err := invoice.NewCreditNote(inv, nil) // full credit, draft
//	if err == nil {
//		err = note.Issue(ctx, seq, correctedAt)
//	}
//
// Multi-tenant applications add WithScope to NewSequence; each tenant then
// counts its series independently and a missing scope fails closed with
// ErrScope. Single-tenant applications pay zero ceremony.
//
// Siblings: money (amounts, allocation), fsm (the lifecycle machine),
// decimal (quantities, rates); planned: ledger (posting refs that payments
// match), fxrate (the snapshot source), tariff and formula (derive the
// amounts that become line items). See docs/packages.md for the package
// catalog.
package invoice
