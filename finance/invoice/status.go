package invoice

import (
	"context"

	"github.com/dmitrymomot/forge/core/fsm"
)

// Status is the document lifecycle state. It lives in the caller's storage
// (a status column) and is moved exclusively by the package operations, which
// fire the package lifecycle machine; an illegal move surfaces the fsm
// package's sentinel errors (fsm.ErrIllegalTransition, fsm.ErrGuardDenied).
type Status string

const (
	// StatusDraft is a mutable, unnumbered work-in-progress document.
	StatusDraft Status = "draft"
	// StatusIssued is a numbered, immutable document awaiting payment.
	StatusIssued Status = "issued"
	// StatusPartiallyPaid is an issued document with payments below total.
	StatusPartiallyPaid Status = "partially_paid"
	// StatusPaid is an issued document whose payments match the total.
	StatusPaid Status = "paid"
	// StatusOverdue is an unpaid or partially paid document past its due date.
	StatusOverdue Status = "overdue"
	// StatusVoid is a cancelled document. Only documents without payments can
	// be voided; corrections to paid documents are credit notes.
	StatusVoid Status = "void"
)

// Kind distinguishes standard invoices from credit notes.
type Kind string

const (
	// KindInvoice is a standard invoice.
	KindInvoice Kind = "invoice"
	// KindCreditNote is a correction document back-referencing the invoice it
	// corrects via Corrects (corrections post forward; issued documents are
	// never edited).
	KindCreditNote Kind = "credit_note"
)

// Direction records who issues the document.
type Direction string

const (
	// DirectionStandard is the normal direction: the supplier issues to the
	// customer.
	DirectionStandard Direction = "standard"
	// DirectionSelfBilled marks a self-billing invoice: the platform issues
	// on the supplier's behalf (affiliate and agent payouts).
	DirectionSelfBilled Direction = "self_billed"
)

var def fsm.Define[Status, *Invoice]

// lifecycle is the compiled document state machine. Voiding is guarded: a
// document that recorded payments can only be corrected forward with a credit
// note, never voided.
var lifecycle = fsm.MustNew(StatusDraft,
	def.Edge(StatusDraft, StatusIssued),
	def.Edge(StatusDraft, StatusVoid),
	def.Edge(StatusIssued, StatusPartiallyPaid),
	def.Edge(StatusIssued, StatusPaid),
	def.Edge(StatusIssued, StatusOverdue),
	def.Edge(StatusIssued, StatusVoid, def.Guard(noPayments)),
	def.Edge(StatusOverdue, StatusPartiallyPaid),
	def.Edge(StatusOverdue, StatusPaid),
	def.Edge(StatusOverdue, StatusVoid, def.Guard(noPayments)),
	def.Edge(StatusPartiallyPaid, StatusPaid),
	def.Edge(StatusPartiallyPaid, StatusOverdue),
	// The guard always denies on these two (their statuses imply payments);
	// the edges exist so every void-with-payments path uniformly reports
	// fsm.ErrGuardDenied wrapping ErrHasPayments.
	def.Edge(StatusPartiallyPaid, StatusVoid, def.Guard(noPayments)),
	def.Edge(StatusPaid, StatusVoid, def.Guard(noPayments)),
)

func noPayments(_ context.Context, inv *Invoice, _, _ Status) error {
	if len(inv.Payments) > 0 {
		return ErrHasPayments
	}
	return nil
}
