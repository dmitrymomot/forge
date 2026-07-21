package invoice_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/fsm"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/finance/invoice"
)

var (
	issueTime = time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	dueTime   = issueTime.AddDate(0, 0, 14)
)

func newSequence(t *testing.T, opts ...invoice.Option) *invoice.Sequence {
	t.Helper()
	seq, err := invoice.NewSequence(invoice.NewMemorySequenceStore(), opts...)
	require.NoError(t, err)
	return seq
}

// draft returns a valid EUR draft: one 100.00 line at 19%, due in 14 days.
func draft(t *testing.T) *invoice.Invoice {
	t.Helper()
	inv := invoice.New("INV-2026", money.EUR)
	inv.Issuer = invoice.Party{Name: "Forge GmbH", TaxID: "DE123456789", Address: []string{"Foundry Lane 1"}}
	inv.Recipient = invoice.Party{Name: "ACME Ltd", Address: []string{"Acme Plaza 9"}}
	inv.Lines = []invoice.LineItem{line(t, "1", "100.00", "0.19", money.EUR)}
	inv.DueAt = dueTime
	return inv
}

func issued(t *testing.T) *invoice.Invoice {
	t.Helper()
	inv := draft(t)
	require.NoError(t, inv.Issue(t.Context(), newSequence(t), issueTime))
	return inv
}

func payment(ref, amount string) invoice.Payment {
	return invoice.Payment{
		Ref:    ref,
		Amount: money.New(decimal.MustParse(amount), money.EUR),
		At:     issueTime.Add(time.Hour),
	}
}

func TestIssue(t *testing.T) {
	t.Parallel()

	inv := draft(t)
	require.NoError(t, inv.Issue(t.Context(), newSequence(t), issueTime))

	assert.Equal(t, "INV-2026-000001", inv.Number)
	assert.Equal(t, invoice.StatusIssued, inv.Status)
	assert.Equal(t, invoice.KindInvoice, inv.Kind)
	assert.Equal(t, invoice.DirectionStandard, inv.Direction)
	assert.Equal(t, issueTime, inv.IssuedAt)
	assertMoney(t, "100.00", inv.Totals.Subtotal)
	assertMoney(t, "19.00", inv.Totals.Tax)
	assertMoney(t, "119.00", inv.Totals.Total)
	assert.NoError(t, inv.Verify())

	// Issued documents are immutable: a second issue is an illegal move.
	assert.ErrorIs(t, inv.Issue(t.Context(), newSequence(t), issueTime), fsm.ErrIllegalTransition)
}

func TestIssue_SequentialNumbers(t *testing.T) {
	t.Parallel()

	seq := newSequence(t)
	for i, want := range []string{"INV-2026-000001", "INV-2026-000002", "INV-2026-000003"} {
		inv := draft(t)
		require.NoError(t, inv.Issue(t.Context(), seq, issueTime), "invoice %d", i)
		assert.Equal(t, want, inv.Number)
	}
}

func TestIssue_ValidationFailureBurnsNoNumber(t *testing.T) {
	t.Parallel()

	seq := newSequence(t)
	inv := draft(t)
	inv.Issuer.Name = ""
	require.ErrorIs(t, inv.Issue(t.Context(), seq, issueTime), invoice.ErrParty)

	// The failed attempt must not have consumed a number.
	inv.Issuer.Name = "Forge GmbH"
	require.NoError(t, inv.Issue(t.Context(), seq, issueTime))
	assert.Equal(t, "INV-2026-000001", inv.Number)
}

func TestIssue_NumberingModes(t *testing.T) {
	t.Parallel()

	t.Run("gapless refuses pre-assigned number", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		inv.Number = "INV-2026-000042"
		err := inv.Issue(t.Context(), newSequence(t), issueTime)
		assert.ErrorIs(t, err, invoice.ErrNumberAssigned)
	})
	t.Run("with-gaps keeps pre-assigned number", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		inv.Number = "INV-2026-000042"
		require.NoError(t, inv.Issue(t.Context(), newSequence(t, invoice.WithMode(invoice.WithGaps)), issueTime))
		assert.Equal(t, "INV-2026-000042", inv.Number)
	})
	t.Run("with-gaps pre-draws via Next", func(t *testing.T) {
		t.Parallel()
		seq := newSequence(t, invoice.WithMode(invoice.WithGaps))
		n, err := seq.Next(t.Context(), "INV-2026")
		require.NoError(t, err)
		assert.Equal(t, "INV-2026-000001", n)

		inv := draft(t)
		inv.Number = n
		require.NoError(t, inv.Issue(t.Context(), seq, issueTime))
		assert.Equal(t, "INV-2026-000001", inv.Number)
	})
	t.Run("nil sequence requires pre-assigned number", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		assert.ErrorIs(t, inv.Issue(t.Context(), nil, issueTime), invoice.ErrNoNumber)

		inv.Number = "EXT-7"
		require.NoError(t, inv.Issue(t.Context(), nil, issueTime))
		assert.Equal(t, "EXT-7", inv.Number)
	})
	t.Run("drawing requires a series", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		inv.Series = ""
		assert.ErrorIs(t, inv.Issue(t.Context(), newSequence(t), issueTime), invoice.ErrNoSeries)
	})
}

func TestIssue_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*invoice.Invoice)
		want   error
	}{
		{"zero issue time is ErrZeroTime", nil, invoice.ErrZeroTime},
		{"unknown kind", func(i *invoice.Invoice) { i.Kind = "receipt" }, invoice.ErrKind},
		{"unknown direction", func(i *invoice.Invoice) { i.Direction = "sideways" }, invoice.ErrDirection},
		{"invoice with corrects ref", func(i *invoice.Invoice) { i.Corrects = "INV-1" }, invoice.ErrCorrects},
		{"credit note without corrects ref", func(i *invoice.Invoice) { i.Kind = invoice.KindCreditNote }, invoice.ErrCorrects},
		{"missing issuer name", func(i *invoice.Invoice) { i.Issuer.Name = "" }, invoice.ErrParty},
		{"missing recipient name", func(i *invoice.Invoice) { i.Recipient.Name = "" }, invoice.ErrParty},
		{"due before issue", func(i *invoice.Invoice) { i.DueAt = issueTime.Add(-time.Hour) }, invoice.ErrDueBeforeIssue},
		{"fx without base", func(i *invoice.Invoice) {
			i.FX = &invoice.FXSnapshot{Rate: decimal.MustParse("1.08")}
		}, invoice.ErrFX},
		{"fx with non-positive rate", func(i *invoice.Invoice) {
			i.FX = &invoice.FXSnapshot{Base: money.USD, Rate: decimal.Zero}
		}, invoice.ErrFX},
		{"draft with payments", func(i *invoice.Invoice) {
			i.Payments = []invoice.Payment{payment("p1", "10.00")}
		}, invoice.ErrHasPayments},
		{"no lines", func(i *invoice.Invoice) { i.Lines = nil }, invoice.ErrNoLines},
		{"lines vs document currency", func(i *invoice.Invoice) { i.Currency = money.USD }, money.ErrCurrencyMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inv := draft(t)
			at := issueTime
			if tt.mutate == nil {
				at = time.Time{}
			} else {
				tt.mutate(inv)
			}
			err := inv.Issue(t.Context(), newSequence(t), at)
			assert.ErrorIs(t, err, tt.want)
			assert.Equal(t, invoice.StatusDraft, inv.Status, "failed issue must not move the document")
		})
	}
}

func TestIssue_WithFXSnapshot(t *testing.T) {
	t.Parallel()

	inv := draft(t)
	inv.FX = &invoice.FXSnapshot{Base: money.USD, Rate: decimal.MustParse("1.0842"), At: issueTime}
	require.NoError(t, inv.Issue(t.Context(), newSequence(t), issueTime))
	assert.Equal(t, "1.0842", inv.FX.Rate.String())
}

func TestApplyPayment(t *testing.T) {
	t.Parallel()

	t.Run("partial then full", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "19.00")))
		assert.Equal(t, invoice.StatusPartiallyPaid, inv.Status)

		out, err := inv.Outstanding()
		require.NoError(t, err)
		assertMoney(t, "100.00", out)

		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:2", "100.00")))
		assert.Equal(t, invoice.StatusPaid, inv.Status)

		paid, err := inv.Paid()
		require.NoError(t, err)
		assertMoney(t, "119.00", paid)
	})
	t.Run("successive partial payments stay partially paid", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		for i, amount := range []string{"19.00", "30.00", "20.00"} {
			require.NoError(t, inv.ApplyPayment(t.Context(), payment(fmt.Sprintf("ledger:%d", i), amount)))
			assert.Equal(t, invoice.StatusPartiallyPaid, inv.Status)
		}
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:final", "50.00")))
		assert.Equal(t, invoice.StatusPaid, inv.Status)
	})
	t.Run("exact single payment pays in full", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "119.00")))
		assert.Equal(t, invoice.StatusPaid, inv.Status)
	})
	t.Run("replay with same amount is a no-op", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "19.00")))
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "19.00")))
		assert.Len(t, inv.Payments, 1)
		assert.Equal(t, invoice.StatusPartiallyPaid, inv.Status)
	})
	t.Run("replay with different amount conflicts", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "19.00")))
		err := inv.ApplyPayment(t.Context(), payment("ledger:1", "20.00"))
		assert.ErrorIs(t, err, invoice.ErrPaymentConflict)
	})
	t.Run("overpayment records nothing", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		err := inv.ApplyPayment(t.Context(), payment("ledger:1", "119.01"))
		assert.ErrorIs(t, err, invoice.ErrOverpayment)
		assert.Empty(t, inv.Payments)
		assert.Equal(t, invoice.StatusIssued, inv.Status)
	})
	t.Run("empty ref", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		assert.ErrorIs(t, inv.ApplyPayment(t.Context(), payment("", "10.00")), invoice.ErrPaymentRef)
	})
	t.Run("non-positive amount", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		assert.ErrorIs(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "0")), invoice.ErrPaymentAmount)
		assert.ErrorIs(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "-5.00")), invoice.ErrPaymentAmount)
	})
	t.Run("sub-minor-unit amount is rejected", func(t *testing.T) {
		t.Parallel()
		// Three 33.333 installments would sum to 99.999 against a rounded
		// total and strand the document in partially_paid forever.
		inv := issued(t)
		err := inv.ApplyPayment(t.Context(), payment("ledger:1", "33.333"))
		assert.ErrorIs(t, err, invoice.ErrPaymentAmount)
		assert.Empty(t, inv.Payments)

		// A scale-heavy but minor-unit-exact amount is fine.
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:2", "33.330")))
		assert.Equal(t, invoice.StatusPartiallyPaid, inv.Status)
	})
	t.Run("currency mismatch", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		p := invoice.Payment{Ref: "ledger:1", Amount: money.FromMinor(1000, money.USD)}
		assert.ErrorIs(t, inv.ApplyPayment(t.Context(), p), money.ErrCurrencyMismatch)
	})
	t.Run("paying a draft is illegal", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		err := inv.ApplyPayment(t.Context(), payment("ledger:1", "10.00"))
		assert.ErrorIs(t, err, fsm.ErrIllegalTransition)
	})
	t.Run("paying a paid document is illegal", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "119.00")))
		err := inv.ApplyPayment(t.Context(), payment("ledger:2", "1.00"))
		assert.ErrorIs(t, err, fsm.ErrIllegalTransition)
	})
	t.Run("replay on a paid document stays idempotent", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "119.00")))
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "119.00")))
		assert.Len(t, inv.Payments, 1)
		assert.Equal(t, invoice.StatusPaid, inv.Status)
	})
	t.Run("paying a void document is illegal", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.Void(t.Context(), issueTime))
		err := inv.ApplyPayment(t.Context(), payment("ledger:1", "10.00"))
		assert.ErrorIs(t, err, fsm.ErrIllegalTransition)
	})
}

func TestMarkOverdue(t *testing.T) {
	t.Parallel()

	t.Run("past due", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.MarkOverdue(t.Context(), dueTime.Add(time.Hour)))
		assert.Equal(t, invoice.StatusOverdue, inv.Status)

		// An overdue document still collects payments.
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "119.00")))
		assert.Equal(t, invoice.StatusPaid, inv.Status)
	})
	t.Run("partially paid goes overdue and back", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "19.00")))
		require.NoError(t, inv.MarkOverdue(t.Context(), dueTime.Add(time.Hour)))
		assert.Equal(t, invoice.StatusOverdue, inv.Status)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:2", "50.00")))
		assert.Equal(t, invoice.StatusPartiallyPaid, inv.Status)
	})
	t.Run("before due date", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		assert.ErrorIs(t, inv.MarkOverdue(t.Context(), dueTime), invoice.ErrNotDue)
	})
	t.Run("no due date", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		inv.DueAt = time.Time{}
		require.NoError(t, inv.Issue(t.Context(), newSequence(t), issueTime))
		assert.ErrorIs(t, inv.MarkOverdue(t.Context(), issueTime.AddDate(1, 0, 0)), invoice.ErrNoDueDate)
	})
	t.Run("already overdue cannot be re-marked", func(t *testing.T) {
		t.Parallel()
		// The caller's sweep selects by status; a naive re-sweep gets a
		// clean illegal-transition error, not silent success.
		inv := issued(t)
		require.NoError(t, inv.MarkOverdue(t.Context(), dueTime.Add(time.Hour)))
		err := inv.MarkOverdue(t.Context(), dueTime.Add(2*time.Hour))
		assert.ErrorIs(t, err, fsm.ErrIllegalTransition)
	})
	t.Run("draft cannot go overdue", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		assert.ErrorIs(t, inv.MarkOverdue(t.Context(), dueTime.Add(time.Hour)), fsm.ErrIllegalTransition)
	})
}

func TestVoid(t *testing.T) {
	t.Parallel()

	t.Run("draft", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		require.NoError(t, inv.Void(t.Context(), issueTime))
		assert.Equal(t, invoice.StatusVoid, inv.Status)
		assert.Equal(t, issueTime, inv.VoidedAt)
	})
	t.Run("zero timestamp", func(t *testing.T) {
		t.Parallel()
		inv := draft(t)
		assert.ErrorIs(t, inv.Void(t.Context(), time.Time{}), invoice.ErrZeroTime)
		assert.Equal(t, invoice.StatusDraft, inv.Status)
	})
	t.Run("issued without payments", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.Void(t.Context(), issueTime.Add(time.Hour)))
		assert.Equal(t, invoice.StatusVoid, inv.Status)
	})
	t.Run("overdue without payments", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.MarkOverdue(t.Context(), dueTime.Add(time.Hour)))
		require.NoError(t, inv.Void(t.Context(), dueTime.Add(2*time.Hour)))
	})
	t.Run("with payments the guard denies", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "19.00")))
		err := inv.Void(t.Context(), dueTime)
		assert.ErrorIs(t, err, fsm.ErrGuardDenied)
		assert.ErrorIs(t, err, invoice.ErrHasPayments)
	})
	t.Run("overdue with payments the guard denies", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "19.00")))
		require.NoError(t, inv.MarkOverdue(t.Context(), dueTime.Add(time.Hour)))
		err := inv.Void(t.Context(), dueTime)
		assert.ErrorIs(t, err, fsm.ErrGuardDenied)
		assert.ErrorIs(t, err, invoice.ErrHasPayments)
	})
	t.Run("paid cannot be voided", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		require.NoError(t, inv.ApplyPayment(t.Context(), payment("ledger:1", "119.00")))
		err := inv.Void(t.Context(), dueTime)
		assert.ErrorIs(t, err, fsm.ErrGuardDenied)
		assert.ErrorIs(t, err, invoice.ErrHasPayments)
	})
}

func TestNewCreditNote(t *testing.T) {
	t.Parallel()

	t.Run("full credit", func(t *testing.T) {
		t.Parallel()
		orig := issued(t)
		note, err := invoice.NewCreditNote(orig, nil)
		require.NoError(t, err)

		assert.Equal(t, invoice.KindCreditNote, note.Kind)
		assert.Equal(t, invoice.StatusDraft, note.Status)
		assert.Equal(t, orig.Number, note.Corrects)
		assert.Equal(t, orig.Series, note.Series)
		assert.Nil(t, note.FX, "fx snapshot is jurisdictional, never inherited")

		// Copied lines are independent of the original document.
		note.Lines[0].Description = "mutated"
		assert.Equal(t, "test line", orig.Lines[0].Description)

		// Party address slices are cloned too: editing the draft note must
		// never reach into the issued, immutable original.
		note.Issuer.Address[0] = "mutated"
		note.Recipient.Address[0] = "mutated"
		assert.Equal(t, "Foundry Lane 1", orig.Issuer.Address[0])
		assert.Equal(t, "Acme Plaza 9", orig.Recipient.Address[0])

		require.NoError(t, note.Issue(t.Context(), newSequence(t), issueTime.AddDate(0, 0, 30)))
		assert.Equal(t, invoice.StatusIssued, note.Status)
		assertMoney(t, "119.00", note.Totals.Total)
	})
	t.Run("partial credit", func(t *testing.T) {
		t.Parallel()
		orig := issued(t)
		note, err := invoice.NewCreditNote(orig, []invoice.LineItem{line(t, "1", "40.00", "0.19", money.EUR)})
		require.NoError(t, err)
		require.NoError(t, note.Issue(t.Context(), newSequence(t), issueTime.AddDate(0, 0, 30)))
		assertMoney(t, "47.60", note.Totals.Total)
	})
	t.Run("credit of a paid original", func(t *testing.T) {
		t.Parallel()
		orig := issued(t)
		require.NoError(t, orig.ApplyPayment(t.Context(), payment("ledger:1", "119.00")))
		_, err := invoice.NewCreditNote(orig, nil)
		assert.NoError(t, err)
	})
	t.Run("exceeding the original", func(t *testing.T) {
		t.Parallel()
		orig := issued(t)
		_, err := invoice.NewCreditNote(orig, []invoice.LineItem{line(t, "1", "200.00", "0.19", money.EUR)})
		assert.ErrorIs(t, err, invoice.ErrCreditExceeds)
	})
	t.Run("draft original", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.NewCreditNote(draft(t), nil)
		assert.ErrorIs(t, err, invoice.ErrNotCreditable)
	})
	t.Run("void original", func(t *testing.T) {
		t.Parallel()
		orig := issued(t)
		require.NoError(t, orig.Void(t.Context(), issueTime))
		_, err := invoice.NewCreditNote(orig, nil)
		assert.ErrorIs(t, err, invoice.ErrNotCreditable)
	})
	t.Run("credit note original", func(t *testing.T) {
		t.Parallel()
		orig := issued(t)
		note, err := invoice.NewCreditNote(orig, nil)
		require.NoError(t, err)
		require.NoError(t, note.Issue(t.Context(), newSequence(t), issueTime.AddDate(0, 0, 30)))
		_, err = invoice.NewCreditNote(note, nil)
		assert.ErrorIs(t, err, invoice.ErrNotCreditable)
	})
	t.Run("nil original", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.NewCreditNote(nil, nil)
		assert.ErrorIs(t, err, invoice.ErrNotCreditable)
	})
	t.Run("currency mismatch", func(t *testing.T) {
		t.Parallel()
		orig := issued(t)
		_, err := invoice.NewCreditNote(orig, []invoice.LineItem{line(t, "1", "10.00", "0.19", money.USD)})
		assert.ErrorIs(t, err, money.ErrCurrencyMismatch)
	})
}

func TestVerify(t *testing.T) {
	t.Parallel()

	t.Run("clean document verifies", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, issued(t).Verify())
	})
	t.Run("tampered line", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		inv.Lines[0].UnitPrice = money.FromMinor(9999, money.EUR)
		assert.ErrorIs(t, inv.Verify(), invoice.ErrTotalsMismatch)
	})
	t.Run("tampered totals", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		inv.Totals.Total = money.FromMinor(1, money.EUR)
		assert.ErrorIs(t, inv.Verify(), invoice.ErrTotalsMismatch)
	})
	t.Run("tampered tax line", func(t *testing.T) {
		t.Parallel()
		inv := issued(t)
		inv.Totals.TaxLines[0].Amount = money.FromMinor(1, money.EUR)
		assert.ErrorIs(t, inv.Verify(), invoice.ErrTotalsMismatch)
	})
}

func TestSelfBilledDirection(t *testing.T) {
	t.Parallel()

	inv := draft(t)
	inv.Direction = invoice.DirectionSelfBilled
	inv.Issuer = invoice.Party{Name: "Platform Inc"}     // platform issues...
	inv.Recipient = invoice.Party{Name: "Affiliate LLC"} // ...on the supplier's behalf
	require.NoError(t, inv.Issue(t.Context(), newSequence(t), issueTime))
	assert.Equal(t, invoice.DirectionSelfBilled, inv.Direction)
}
