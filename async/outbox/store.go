package outbox

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

// Store is the outbox storage seam: intent rows live in the business
// database so Add can ride the caller's transaction. It is strictly pull —
// the relay owns polling cadence, batching, and retry backoff.
//
// Contract details every implementation must honor:
//   - Add inserts one row per job inside the caller's transaction; tx is
//     driver-specific (the postgres store asserts pgx.Tx). An empty batch is
//     a no-op. Rows become claimable only when the transaction commits.
//   - Claim picks up to n committed rows that are due (not leased, not in
//     retry backoff), most-overdue first — smallest (available time, id), so
//     fresh rows go in insert order and failed rows re-enter at their retry
//     time — returns the batch ordered by (created_at, id), increments each
//     row's Attempts, and hides claimed rows from other relays for lease.
//   - Delete removes rows by id; unknown ids are ignored — a row already
//     forwarded and deleted by a competing relay must not fail the batch.
//   - Fail reschedules a row: claimable again no earlier than retryAt, with
//     reason recorded as LastError. Unknown ids are ignored.
//   - Stats reports the backlog for observability.
//
// Claims are deliberately unfenced (no claim token, unlike queue.Broker): the
// relay's post-claim window is one push plus one delete, and every lost-lease
// interleaving degrades to a duplicate push — which the at-least-once
// delivery contract already permits.
type Store interface {
	Add(ctx context.Context, tx any, jobs ...queue.Job) error
	Claim(ctx context.Context, n int, lease time.Duration) ([]Entry, error)
	Delete(ctx context.Context, ids ...string) error
	Fail(ctx context.Context, id string, retryAt time.Time, reason string) error
	Stats(ctx context.Context) (Stats, error)
}

// Entry is one stored intent row: the job envelope exactly as produced, plus
// the outbox's own forwarding state. Attempts counts relay claims of this row
// (a row being forwarded for the first time has Attempts == 1); it is
// independent of Job.Attempt, which stays zero until the queue engine claims
// the job after forwarding.
type Entry struct {
	LastError string
	Job       queue.Job
	Attempts  int
}

// Stats is the outbox backlog snapshot. Pending counts rows not yet
// forwarded; implementations may bound the count (postgres caps at 10000)
// and set PendingCapped. Oldest is the created_at of the oldest pending row
// (zero when empty) — its age is the relay lag.
type Stats struct {
	Oldest        time.Time
	Pending       int
	PendingCapped bool
}
