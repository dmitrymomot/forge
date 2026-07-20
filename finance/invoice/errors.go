package invoice

import "errors"

var (
	// ErrNoLines reports an invoice with no line items.
	ErrNoLines = errors.New("invoice: no line items")
	// ErrInvalidLine reports a line item with a non-positive quantity, a
	// negative unit price, or a negative tax rate.
	ErrInvalidLine = errors.New("invoice: invalid line item")
	// ErrRoundingPolicy reports an unknown RoundingPolicy value.
	ErrRoundingPolicy = errors.New("invoice: unknown rounding policy")
	// ErrKind reports an unknown document Kind.
	ErrKind = errors.New("invoice: unknown document kind")
	// ErrDirection reports an unknown Direction.
	ErrDirection = errors.New("invoice: unknown direction")
	// ErrCorrects reports a Corrects reference that contradicts the document
	// kind: a credit note must reference the invoice it corrects, and a
	// standard invoice must not carry a Corrects reference.
	ErrCorrects = errors.New("invoice: corrects reference mismatch")
	// ErrParty reports a missing issuer or recipient name at issue time.
	ErrParty = errors.New("invoice: missing party name")
	// ErrZeroTime reports a zero timestamp passed to Issue or Void.
	ErrZeroTime = errors.New("invoice: zero timestamp")
	// ErrDueBeforeIssue reports a due date earlier than the issue date.
	ErrDueBeforeIssue = errors.New("invoice: due date before issue date")
	// ErrFX reports an FX snapshot with a missing base currency or a
	// non-positive rate.
	ErrFX = errors.New("invoice: invalid fx snapshot")

	// ErrNoNumber reports issuing without a Sequence when no number was
	// pre-assigned, or a document that unexpectedly has no number.
	ErrNoNumber = errors.New("invoice: no number assigned")
	// ErrNumberAssigned reports a pre-assigned number on a gapless series:
	// gapless numbering draws exclusively at issue time.
	ErrNumberAssigned = errors.New("invoice: number pre-assigned on gapless series")
	// ErrNoSeries reports drawing a number for an invoice with an empty
	// Series.
	ErrNoSeries = errors.New("invoice: no series")
	// ErrGapless reports a pre-draw attempt (Sequence.Next) on a gapless
	// sequence, which draws numbers only inside Issue.
	ErrGapless = errors.New("invoice: gapless sequence draws numbers only at issue")
	// ErrScope reports a configured scope hook that failed or returned empty.
	ErrScope = errors.New("invoice: scope resolution failed")

	// ErrHasPayments reports an operation that requires an unpaid document
	// (voiding, issuing) on a document that already recorded payments.
	ErrHasPayments = errors.New("invoice: payments recorded")
	// ErrPaymentRef reports a payment with an empty Ref.
	ErrPaymentRef = errors.New("invoice: missing payment ref")
	// ErrPaymentConflict reports a payment Ref replayed with a different
	// amount. An identical replay (same Ref, same amount) is a no-op.
	ErrPaymentConflict = errors.New("invoice: payment ref reused with different amount")
	// ErrPaymentAmount reports a non-positive payment amount, or one not
	// representable in the currency's minor units (totals are minor-unit
	// precise; a sub-minor-unit payment could never reconcile against them).
	ErrPaymentAmount = errors.New("invoice: invalid payment amount")
	// ErrOverpayment reports a payment that would push the paid sum above the
	// invoice total.
	ErrOverpayment = errors.New("invoice: payments exceed total")

	// ErrNoDueDate reports MarkOverdue on a document without a due date.
	ErrNoDueDate = errors.New("invoice: no due date")
	// ErrNotDue reports MarkOverdue before the due date has passed.
	ErrNotDue = errors.New("invoice: due date not passed")

	// ErrNotCreditable reports a credit note against a document that cannot
	// be corrected: a draft, a void document, or another credit note.
	ErrNotCreditable = errors.New("invoice: original not creditable")
	// ErrCreditExceeds reports a credit note whose total exceeds the original
	// invoice total.
	ErrCreditExceeds = errors.New("invoice: credit note exceeds original total")

	// ErrTotalsMismatch reports stored totals that no longer match the
	// document's line items (Verify).
	ErrTotalsMismatch = errors.New("invoice: stored totals do not match lines")
)
