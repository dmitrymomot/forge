//go:build integration

package pgoutbox_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/outbox"
	pgoutbox "github.com/dmitrymomot/forge/async/outbox/postgres"
	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ outbox.Store = (*pgoutbox.Store)(nil)

// openPool connects to the suite's Postgres (via pgtest.DSN) and applies the
// outbox migration.
func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(tb)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	tb.Cleanup(func() { _ = db.Close() })
	require.NoError(tb, migration.New(pgoutbox.Migrations, migration.WithTable("forge_outbox_schema")).Up(context.Background(), db))
	return pool
}

func newStore(tb testing.TB, pool *pgxpool.Pool) *pgoutbox.Store {
	tb.Helper()
	_, err := pool.Exec(context.Background(), "TRUNCATE outbox_jobs")
	require.NoError(tb, err)
	s, err := pgoutbox.New(pool)
	require.NoError(tb, err)
	return s
}

func makeJob(created time.Time) queue.Job {
	return queue.Job{
		ID:        id.NewUUID().String(),
		Queue:     "default",
		Type:      "test.job",
		Scope:     "tenant-1",
		Payload:   []byte(`{"n":1}`),
		RunAt:     created.Add(time.Minute),
		CreatedAt: created,
	}
}

// claimN polls Claim until it yields want entries or a deadline passes,
// tolerating a containerised database clock that lags the test process.
func claimN(t *testing.T, s *pgoutbox.Store, want int, lease time.Duration) []outbox.Entry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := s.Claim(context.Background(), max(want, 10), lease)
		require.NoError(t, err)
		if len(got) >= want || time.Now().After(deadline) {
			require.Len(t, got, want)
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestPgOutbox_New(t *testing.T) {
	pool := openPool(t)

	_, err := pgoutbox.New(nil)
	require.ErrorContains(t, err, "nil pool")

	_, err = pgoutbox.New(pool, pgoutbox.WithTable("bad name"))
	require.ErrorContains(t, err, "invalid table name")

	_, err = pgoutbox.New(pool, pgoutbox.WithTable("outbox_jobs; DROP TABLE users"))
	require.ErrorContains(t, err, "invalid table name")
}

func TestPgOutbox_AddCommitVisibility(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	j1 := makeJob(now)
	j2 := makeJob(now.Add(time.Second))

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, s.Add(ctx, tx, j1, j2))

	got, err := s.Claim(ctx, 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "uncommitted rows are invisible")

	require.NoError(t, tx.Commit(ctx))
	entries := claimN(t, s, 2, time.Minute)
	assert.Equal(t, j1.ID, entries[0].Job.ID, "ordered by created_at")
	assert.Equal(t, j2.ID, entries[1].Job.ID)

	e := entries[0]
	assert.Equal(t, 1, e.Attempts)
	assert.Empty(t, e.LastError)
	assert.Equal(t, j1.Queue, e.Job.Queue)
	assert.Equal(t, j1.Type, e.Job.Type)
	assert.Equal(t, j1.Scope, e.Job.Scope, "tenant scope rides the envelope verbatim")
	assert.JSONEq(t, string(j1.Payload), string(e.Job.Payload))
	assert.True(t, e.Job.RunAt.Equal(j1.RunAt), "future RunAt is preserved for the queue engine")
	assert.True(t, e.Job.CreatedAt.Equal(j1.CreatedAt))
	assert.Zero(t, e.Job.Attempt, "forwarded envelope starts with a fresh attempt budget")
}

func TestPgOutbox_AddRollback(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, s.Add(ctx, tx, makeJob(time.Now().UTC())))
	require.NoError(t, tx.Rollback(ctx))

	st, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, st.Pending, "rolled-back intent rows vanish")
}

func TestPgOutbox_AddRequiresPgxTx(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)

	err := s.Add(context.Background(), nil, makeJob(time.Now().UTC()))
	require.ErrorContains(t, err, "expected pgx.Tx")

	err = s.Add(context.Background(), "not a tx", makeJob(time.Now().UTC()))
	require.ErrorContains(t, err, "expected pgx.Tx")
}

func TestPgOutbox_AddEmptyBatch(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	require.NoError(t, pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return s.Add(context.Background(), tx)
	}))
}

func addCommitted(t *testing.T, pool *pgxpool.Pool, s *pgoutbox.Store, jobs ...queue.Job) {
	t.Helper()
	require.NoError(t, pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return s.Add(context.Background(), tx, jobs...)
	}))
}

func TestPgOutbox_ClaimLease(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	ctx := context.Background()

	addCommitted(t, pool, s, makeJob(time.Now().UTC()))
	claimN(t, s, 1, time.Minute)

	got, err := s.Claim(ctx, 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "leased row is hidden from other claims")
}

func TestPgOutbox_LeaseExpiryReclaim(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)

	addCommitted(t, pool, s, makeJob(time.Now().UTC()))
	claimN(t, s, 1, 50*time.Millisecond)

	entries := claimN(t, s, 1, time.Minute) // polls past the 50ms lease
	assert.Equal(t, 2, entries[0].Attempts, "reclaim after lease expiry increments attempts")
}

func TestPgOutbox_ClaimNonPositive(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	addCommitted(t, pool, s, makeJob(time.Now().UTC()))

	got, err := s.Claim(context.Background(), 0, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPgOutbox_FailReschedules(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	ctx := context.Background()

	j := makeJob(time.Now().UTC())
	addCommitted(t, pool, s, j)
	claimN(t, s, 1, time.Minute)

	require.NoError(t, s.Fail(ctx, j.ID, time.Now().UTC().Add(time.Hour), "boom"))
	got, err := s.Claim(ctx, 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "row in backoff is hidden")

	require.NoError(t, s.Fail(ctx, j.ID, time.Now().UTC().Add(-time.Second), "boom again"))
	entries := claimN(t, s, 1, time.Minute)
	assert.Equal(t, 2, entries[0].Attempts)
	assert.Equal(t, "boom again", entries[0].LastError)

	require.NoError(t, s.Fail(ctx, id.NewUUID().String(), time.Now().UTC(), "ghost"), "unknown id is ignored")
}

func TestPgOutbox_PickByOverdueReturnByCreated(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	ctx := context.Background()

	// Three rows in created order r1 < r2 < r3, then a retry backlog that
	// reverses their availability: r3 most overdue, r1 least. Offsets are in
	// minutes so a lagging container DB clock cannot push them into the future.
	now := time.Now().UTC().Truncate(time.Microsecond)
	r1 := makeJob(now)
	r2 := makeJob(now.Add(time.Second))
	r3 := makeJob(now.Add(2 * time.Second))
	addCommitted(t, pool, s, r1, r2, r3)
	claimN(t, s, 3, time.Minute)
	require.NoError(t, s.Fail(ctx, r1.ID, now.Add(-10*time.Minute), "later"))
	require.NoError(t, s.Fail(ctx, r2.ID, now.Add(-20*time.Minute), "later"))
	require.NoError(t, s.Fail(ctx, r3.ID, now.Add(-30*time.Minute), "later"))

	// Pick order is most-overdue first (r3, r2); return order is (created, id).
	got, err := s.Claim(ctx, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, r2.ID, got[0].Job.ID)
	assert.Equal(t, r3.ID, got[1].Job.ID)

	got, err = s.Claim(ctx, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1, "r1 was least overdue and must not have been picked")
	assert.Equal(t, r1.ID, got[0].Job.ID)
}

func TestPgOutbox_Delete(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	ctx := context.Background()

	j1 := makeJob(time.Now().UTC())
	j2 := makeJob(time.Now().UTC())
	addCommitted(t, pool, s, j1, j2)

	require.NoError(t, s.Delete(ctx, j1.ID, id.NewUUID().String()))
	st, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Pending)

	require.NoError(t, s.Delete(ctx), "empty batch is a no-op")
}

func TestPgOutbox_Stats(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	ctx := context.Background()

	st, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, outbox.Stats{}, st)

	now := time.Now().UTC().Truncate(time.Microsecond)
	addCommitted(t, pool, s, makeJob(now.Add(time.Second)), makeJob(now))
	st, err = s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, st.Pending)
	assert.False(t, st.PendingCapped)
	assert.True(t, st.Oldest.Equal(now), "oldest is the earliest created_at")
}

func TestPgOutbox_WithTable(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	newStore(t, pool) // truncate the default table so the isolation assertion below is meaningful
	_, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS outbox_custom (LIKE outbox_jobs INCLUDING ALL)")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "TRUNCATE outbox_custom")
	require.NoError(t, err)

	s, err := pgoutbox.New(pool, pgoutbox.WithTable("outbox_custom"))
	require.NoError(t, err)
	addCommitted(t, pool, s, makeJob(time.Now().UTC()))
	claimN(t, s, 1, time.Minute)

	var n int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM outbox_jobs").Scan(&n))
	assert.Zero(t, n, "default table untouched")
}

func TestPgOutbox_RelayEndToEnd(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)
	broker := queue.NewMemoryBroker()

	jobs := make([]queue.Job, 5)
	now := time.Now().UTC()
	for i := range jobs {
		jobs[i] = makeJob(now.Add(time.Duration(i) * time.Millisecond))
	}
	addCommitted(t, pool, s, jobs...)

	relay, err := outbox.NewRelay(s, broker, outbox.WithConfig(outbox.Config{
		BatchSize: 2, PollInterval: 10 * time.Millisecond, Lease: time.Minute,
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = relay.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	require.Eventually(t, func() bool {
		bst, err := broker.Stats(context.Background())
		require.NoError(t, err)
		st, err := s.Stats(context.Background())
		require.NoError(t, err)
		return bst["default"].Pending == 5 && st.Pending == 0
	}, 10*time.Second, 25*time.Millisecond, "relay drains the pg outbox into the broker")
}

func TestPgOutbox_ConcurrentClaimNoOverlap(t *testing.T) {
	pool := openPool(t)
	s := newStore(t, pool)

	now := time.Now().UTC()
	jobs := make([]queue.Job, 20)
	for i := range jobs {
		jobs[i] = makeJob(now.Add(time.Duration(i) * time.Millisecond))
	}
	addCommitted(t, pool, s, jobs...)

	// Wait until all rows are visible, then claim concurrently.
	claimed := claimN(t, s, 10, time.Millisecond)
	_ = claimed
	time.Sleep(50 * time.Millisecond) // let the 1ms lease lapse so all 20 are claimable again

	const workers = 4
	results := make(chan []outbox.Entry, workers)
	for range workers {
		go func() {
			got, err := s.Claim(context.Background(), 10, time.Minute)
			assert.NoError(t, err)
			results <- got
		}()
	}
	seen := make(map[string]int)
	total := 0
	for range workers {
		for _, e := range <-results {
			seen[e.Job.ID]++
			total++
		}
	}
	assert.Equal(t, 20, total)
	for jid, n := range seen {
		assert.Equal(t, 1, n, "job %s claimed by exactly one worker (%s)", jid, strconv.Itoa(n))
	}
}
