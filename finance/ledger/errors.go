package ledger

import "errors"

var (
	// ErrScopeMissing is returned when a scope hook is configured and yields an error or empty scope.
	ErrScopeMissing = errors.New("ledger: scope missing")
	// ErrInvalidRef is returned when a ref (posting, hold, group back-reference) is empty.
	ErrInvalidRef = errors.New("ledger: invalid ref")
	// ErrInvalidAmount is returned when an amount is zero or negative.
	ErrInvalidAmount = errors.New("ledger: invalid amount")
	// ErrInvalidKey is returned by EnsureAccount when the account key has empty fields.
	ErrInvalidKey = errors.New("ledger: invalid account key")
	// ErrSameAccount is returned when a posting or settle names the same account on both sides.
	ErrSameAccount = errors.New("ledger: src and dst are the same account")
	// ErrAccountNotFound is returned when an account does not exist under the resolved tenant.
	ErrAccountNotFound = errors.New("ledger: account not found")
	// ErrCurrencyMismatch is returned when an amount's currency differs from the account's.
	ErrCurrencyMismatch = errors.New("ledger: currency mismatch")
	// ErrInsufficientFunds is returned when a post or hold would take available below the account floor.
	ErrInsufficientFunds = errors.New("ledger: insufficient funds")
	// ErrHoldNotFound is returned by Settle and Void when no hold carries the ref.
	ErrHoldNotFound = errors.New("ledger: hold not found")
	// ErrPostingNotFound is returned by PostingByRef when no posting carries the ref.
	ErrPostingNotFound = errors.New("ledger: posting not found")
	// ErrAlreadySettled is returned by Void when the hold has already settled.
	ErrAlreadySettled = errors.New("ledger: hold already settled")
	// ErrAlreadyVoided is returned by Settle when the hold has already been voided.
	ErrAlreadyVoided = errors.New("ledger: hold already voided")
	// ErrExceedsHold is returned by Settle when the settle amount exceeds the held amount.
	ErrExceedsHold = errors.New("ledger: settle amount exceeds hold")
	// ErrRefConflict is returned when a ref replays with different parameters than the original.
	ErrRefConflict = errors.New("ledger: ref replayed with different parameters")
	// ErrRefRace is returned when a concurrent transaction inserted the same ref first; the caller's
	// transaction is aborted — retry the transaction to observe the idempotent replay.
	ErrRefRace = errors.New("ledger: ref inserted concurrently, retry the transaction")
)
