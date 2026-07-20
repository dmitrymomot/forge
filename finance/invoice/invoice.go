package invoice

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
)

// Party identifies one side of the document. It is inert data frozen with
// the rest of the document at issue; only Name is required then.
type Party struct {
	// Name is the legal name. Required at issue.
	Name string
	// TaxID is the party's tax identifier (VAT number, EIN). Optional.
	TaxID string
	// Address holds free-form address lines. Optional.
	Address []string
}

// FXSnapshot records the exchange rate that applied when the document was
// issued in a currency other than the ledger's or report's base currency. It
// is recorded data — the package does no conversion math — so audits can
// answer "what rate at issue time".
type FXSnapshot struct {
	// At is when the rate was observed.
	At time.Time
	// Base is the reporting currency the rate converts into.
	Base money.Currency
	// Rate is the price of one unit of the document currency in Base units.
	// Must be positive.
	Rate decimal.Decimal
}

// Payment matches money received against the document, keyed by an external
// reference — a ledger posting ref or a PSP transaction id. Refs are the
// idempotency key: replaying a ref with the same amount is a no-op, with a
// different amount an ErrPaymentConflict.
type Payment struct {
	// At is when the payment was received. Inert data.
	At time.Time
	// Ref is the external posting reference. Required, unique per document.
	Ref string
	// Amount is the paid amount in the document currency. Must be positive.
	Amount money.Money
}

// Invoice is the document model: a plain, persistable struct whose
// invariants are enforced at the package's operation boundary, mirroring the
// fsm philosophy of caller-owned state — the caller stores the document, the
// package's operations are the only legal way to move it. A draft is freely
// mutable; Issue freezes it (number, totals, parties, lines), and every
// later correction is a new credit-note document, never an edit. Verify
// re-checks a loaded document's totals against its lines to detect drift.
type Invoice struct {
	// IssuedAt is set by Issue, VoidedAt by Void.
	IssuedAt time.Time
	// DueAt is the caller-set due date; zero means none (MarkOverdue then
	// returns ErrNoDueDate).
	DueAt    time.Time
	VoidedAt time.Time
	// FX optionally records the exchange-rate snapshot for documents issued
	// in a non-base currency. Recorded, never computed.
	FX *FXSnapshot
	// Series is the numbering series the document belongs to, e.g.
	// "INV-2026". Required when Issue draws the number.
	Series string
	// Number is the formatted document number. Assigned by Issue; may be
	// pre-assigned only on WithGaps series or when issuing without a
	// Sequence.
	Number string
	// Kind is invoice vs credit note. New and NewCreditNote set it; Issue
	// defaults empty to KindInvoice.
	Kind Kind
	// Direction records who issues; Issue defaults empty to
	// DirectionStandard.
	Direction Direction
	// Status is the lifecycle state, moved only by package operations.
	Status Status
	// Corrects back-references the corrected invoice's Number. Required on
	// credit notes, forbidden on invoices.
	Corrects string
	// Totals is computed and frozen by Issue.
	Totals Totals
	// Issuer and Recipient are the document parties.
	Issuer    Party
	Recipient Party
	// Currency is the document currency; every line and payment must match.
	Currency money.Currency
	// Lines are the line items. Mutable while draft.
	Lines []LineItem
	// Payments are the applied payments, appended by ApplyPayment.
	Payments []Payment
	// Rounding selects the totals rounding policy. Zero value RoundPerLine.
	Rounding RoundingPolicy
}

// New returns a draft invoice in the given numbering series and currency.
func New(series string, c money.Currency) *Invoice {
	return &Invoice{
		Series:    series,
		Kind:      KindInvoice,
		Direction: DirectionStandard,
		Status:    StatusDraft,
		Currency:  c,
	}
}

// Issue validates the draft, freezes its totals, assigns the document
// number, and moves it draft → issued as of at. From this point the document
// is immutable; corrections are credit notes.
//
// Numbering: with a Gapless sequence the number is drawn here — inside the
// caller's persistence transaction when the store rides ctx — and a
// pre-assigned Number is refused with ErrNumberAssigned. With a WithGaps
// sequence a pre-assigned Number is kept, otherwise one is drawn. With seq
// nil the caller numbers documents itself and Number must already be set.
func (inv *Invoice) Issue(ctx context.Context, seq *Sequence, at time.Time) error {
	if err := lifecycle.Fire(ctx, inv, inv.Status, StatusIssued); err != nil {
		return err
	}
	if at.IsZero() {
		return ErrIssueTime
	}
	kind := inv.Kind
	if kind == "" {
		kind = KindInvoice
	}
	if kind != KindInvoice && kind != KindCreditNote {
		return fmt.Errorf("%w: %q", ErrKind, inv.Kind)
	}
	dir := inv.Direction
	if dir == "" {
		dir = DirectionStandard
	}
	if dir != DirectionStandard && dir != DirectionSelfBilled {
		return fmt.Errorf("%w: %q", ErrDirection, inv.Direction)
	}
	if kind == KindCreditNote && inv.Corrects == "" {
		return fmt.Errorf("%w: credit note without corrects reference", ErrCorrects)
	}
	if kind == KindInvoice && inv.Corrects != "" {
		return fmt.Errorf("%w: standard invoice with corrects reference", ErrCorrects)
	}
	if inv.Issuer.Name == "" || inv.Recipient.Name == "" {
		return ErrParty
	}
	if !inv.DueAt.IsZero() && inv.DueAt.Before(at) {
		return ErrDueBeforeIssue
	}
	if inv.FX != nil && (inv.FX.Base.Code == "" || !inv.FX.Rate.IsPositive()) {
		return ErrFX
	}
	if len(inv.Payments) != 0 {
		return ErrHasPayments
	}

	totals, err := Compute(inv.Lines, inv.Rounding)
	if err != nil {
		return err
	}
	if totals.Total.Currency().Code != inv.Currency.Code {
		return fmt.Errorf("invoice: lines vs document: %w", money.ErrCurrencyMismatch)
	}

	number := inv.Number
	switch {
	case seq == nil:
		if number == "" {
			return ErrNoNumber
		}
	case number != "":
		if seq.Mode() == Gapless {
			return ErrNumberAssigned
		}
	default:
		if inv.Series == "" {
			return ErrNoSeries
		}
		// The number is drawn last, only after every other invariant
		// passed, so a validation failure can never burn one.
		if number, err = seq.next(ctx, inv.Series); err != nil {
			return err
		}
	}

	inv.Kind = kind
	inv.Direction = dir
	inv.Totals = totals
	inv.Number = number
	inv.IssuedAt = at
	inv.Status = StatusIssued
	return nil
}

// ApplyPayment records a payment against an issued document and moves it to
// partially_paid or paid. Matching is by Ref (a ledger posting ref):
// replaying an already-applied Ref with the same amount is an idempotent
// no-op, so at-least-once payment feeds are safe; the same Ref with a
// different amount is ErrPaymentConflict. A payment pushing the paid sum
// above the total is ErrOverpayment and records nothing.
func (inv *Invoice) ApplyPayment(ctx context.Context, p Payment) error {
	if p.Ref == "" {
		return ErrPaymentRef
	}
	if !p.Amount.IsPositive() {
		return ErrPaymentAmount
	}
	if p.Amount.Currency().Code != inv.Currency.Code {
		return fmt.Errorf("invoice: payment %q: %w", p.Ref, money.ErrCurrencyMismatch)
	}
	for _, applied := range inv.Payments {
		if applied.Ref == p.Ref {
			if eq, err := applied.Amount.Equal(p.Amount); err == nil && eq {
				return nil
			}
			return fmt.Errorf("%w: %q", ErrPaymentConflict, p.Ref)
		}
	}
	paid, err := inv.Paid()
	if err != nil {
		return err
	}
	if paid, err = paid.Add(p.Amount); err != nil {
		return err
	}
	c, err := paid.Cmp(inv.Totals.Total)
	if err != nil {
		return err
	}
	if c > 0 {
		return fmt.Errorf("%w: ref %q", ErrOverpayment, p.Ref)
	}
	target := StatusPartiallyPaid
	if c == 0 {
		target = StatusPaid
	}
	// A further partial payment on a partially paid document is not a state
	// transition; only actual moves fire the machine.
	if target != inv.Status {
		if err := lifecycle.Fire(ctx, inv, inv.Status, target); err != nil {
			return err
		}
	}
	inv.Payments = append(inv.Payments, p)
	inv.Status = target
	return nil
}

// MarkOverdue moves an unpaid or partially paid document past its due date
// to overdue. It is the caller's sweep that decides when to check; now must
// be strictly after DueAt.
func (inv *Invoice) MarkOverdue(ctx context.Context, now time.Time) error {
	if inv.DueAt.IsZero() {
		return ErrNoDueDate
	}
	if !now.After(inv.DueAt) {
		return ErrNotDue
	}
	if err := lifecycle.Fire(ctx, inv, inv.Status, StatusOverdue); err != nil {
		return err
	}
	inv.Status = StatusOverdue
	return nil
}

// Void cancels the document as of at. Only documents without payments can be
// voided (the lifecycle guard denies with ErrHasPayments); once money moved,
// the correction is a credit note.
func (inv *Invoice) Void(ctx context.Context, at time.Time) error {
	if err := lifecycle.Fire(ctx, inv, inv.Status, StatusVoid); err != nil {
		return err
	}
	inv.Status = StatusVoid
	inv.VoidedAt = at
	return nil
}

// Paid returns the exact sum of applied payments, zero in the document
// currency when there are none.
func (inv *Invoice) Paid() (money.Money, error) {
	total := money.FromMinor(0, inv.Currency)
	for _, p := range inv.Payments {
		var err error
		if total, err = total.Add(p.Amount); err != nil {
			return money.Money{}, err
		}
	}
	return total, nil
}

// Outstanding returns Total minus Paid. It is never negative for documents
// maintained through ApplyPayment, which refuses overpayment.
func (inv *Invoice) Outstanding() (money.Money, error) {
	paid, err := inv.Paid()
	if err != nil {
		return money.Money{}, err
	}
	return inv.Totals.Total.Sub(paid)
}

// Verify recomputes totals from the document's lines and compares them with
// the stored Totals, returning ErrTotalsMismatch on any difference. Run it
// on documents loaded from storage to detect drift: an issued document whose
// lines or totals were edited behind the package's back no longer verifies.
func (inv *Invoice) Verify() error {
	totals, err := Compute(inv.Lines, inv.Rounding)
	if err != nil {
		return err
	}
	if !moneyEq(totals.Subtotal, inv.Totals.Subtotal) ||
		!moneyEq(totals.Tax, inv.Totals.Tax) ||
		!moneyEq(totals.Total, inv.Totals.Total) {
		return ErrTotalsMismatch
	}
	if len(totals.LineNets) != len(inv.Totals.LineNets) ||
		len(totals.TaxLines) != len(inv.Totals.TaxLines) {
		return ErrTotalsMismatch
	}
	for i := range totals.LineNets {
		if !moneyEq(totals.LineNets[i], inv.Totals.LineNets[i]) {
			return ErrTotalsMismatch
		}
	}
	for i := range totals.TaxLines {
		want, got := totals.TaxLines[i], inv.Totals.TaxLines[i]
		if want.Rate.Cmp(got.Rate) != 0 || !moneyEq(want.Base, got.Base) || !moneyEq(want.Amount, got.Amount) {
			return ErrTotalsMismatch
		}
	}
	return nil
}

// NewCreditNote builds a draft credit note correcting original. With nil or
// empty lines it credits the original in full (lines are copied); explicit
// lines make a partial credit. The draft back-references original.Number via
// Corrects and inherits series, parties, currency, direction, and rounding —
// all overridable before Issue (credit notes commonly run their own series).
// FX is not inherited: whether a correction uses the original's rate or the
// current one is jurisdictional, so the caller sets it.
//
// The credit total is checked against the original's total here, while the
// original is in hand; the package is stateless, so keeping cumulative
// partial credits within the original total is the caller's check.
func NewCreditNote(original *Invoice, lines []LineItem) (*Invoice, error) {
	if original == nil {
		return nil, ErrNotCreditable
	}
	if original.Kind == KindCreditNote {
		return nil, fmt.Errorf("%w: cannot credit a credit note", ErrNotCreditable)
	}
	switch original.Status {
	case StatusIssued, StatusPartiallyPaid, StatusPaid, StatusOverdue:
	default:
		return nil, fmt.Errorf("%w: status %q", ErrNotCreditable, original.Status)
	}
	if original.Number == "" {
		return nil, ErrNoNumber
	}
	if len(lines) == 0 {
		lines = original.Lines
	}
	lines = slices.Clone(lines)
	totals, err := Compute(lines, original.Rounding)
	if err != nil {
		return nil, err
	}
	if totals.Total.Currency().Code != original.Currency.Code {
		return nil, fmt.Errorf("invoice: credit note vs original: %w", money.ErrCurrencyMismatch)
	}
	over, err := totals.Total.GreaterThan(original.Totals.Total)
	if err != nil {
		return nil, err
	}
	if over {
		return nil, ErrCreditExceeds
	}
	return &Invoice{
		Series:    original.Series,
		Kind:      KindCreditNote,
		Direction: original.Direction,
		Status:    StatusDraft,
		Issuer:    original.Issuer,
		Recipient: original.Recipient,
		Currency:  original.Currency,
		Lines:     lines,
		Rounding:  original.Rounding,
		Corrects:  original.Number,
	}, nil
}

func moneyEq(a, b money.Money) bool {
	eq, err := a.Equal(b)
	return err == nil && eq
}
