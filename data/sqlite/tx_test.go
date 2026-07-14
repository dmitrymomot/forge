package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func txDB(t *testing.T) (*sqlite.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tx.db")
	cfg := sqlite.DefaultConfig()
	cfg.Path = path
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlite.Close(db, nil) })
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	return db, path
}

func count(t *testing.T, db *sqlite.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestWithTx_CommitsOnSuccess(t *testing.T) {
	db, _ := txDB(t)
	err := sqlite.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO t(id) VALUES (1)`)
		return e
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if count(t, db) != 1 {
		t.Fatalf("row not committed")
	}
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	db, _ := txDB(t)
	sentinel := errors.New("nope")
	err := sqlite.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO t(id) VALUES (1)`)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if count(t, db) != 0 {
		t.Fatalf("row must have rolled back")
	}
}

func TestWithTx_RollsBackAndRepanics(t *testing.T) {
	db, _ := txDB(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("panic must propagate")
		}
		if count(t, db) != 0 {
			t.Fatalf("row must have rolled back on panic")
		}
	}()
	_ = sqlite.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO t(id) VALUES (1)`)
		panic("boom")
	})
}

// holdWriteLock opens an independent connection with busy_timeout=0, begins an
// immediate transaction that takes the write lock, and returns a release func. Any
// writer that also has busy_timeout=0 busies out immediately while the lock is held.
func holdWriteLock(t *testing.T, path string) (release func()) {
	t.Helper()
	blocker, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("blocker open: %v", err)
	}
	held, err := blocker.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("blocker begin: %v", err)
	}
	if _, err := held.Exec(`INSERT INTO t(id) VALUES (999)`); err != nil {
		t.Fatalf("blocker write: %v", err)
	}
	return func() {
		_ = held.Rollback()
		_ = blocker.Close()
	}
}

// fastWriter opens db against path with BusyTimeout=0, so any writer contention
// busies out immediately instead of waiting.
func fastWriter(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	cfg := sqlite.DefaultConfig()
	cfg.Path = path
	cfg.BusyTimeout = 0
	fast, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("fast open: %v", err)
	}
	t.Cleanup(func() { sqlite.Close(fast, nil) })
	return fast
}

func TestWithRetryAttempts_BelowOneClampsToOneAttempt(t *testing.T) {
	// With busy_timeout=0 and the write lock held, BeginTx itself fails immediately
	// with SQLITE_BUSY, so fn is never invoked (WithTx only calls fn once BeginTx
	// succeeds) — attempt count is not observable via fn. It IS observable via
	// timing: a single attempt returns immediately (no backoff wait), while 2+
	// attempts wait ~50ms (the default interval) between them. So a clamp to 1
	// attempt is the only value of n that returns near-instantly here.
	for _, n := range []int{0, -5} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			_, path := txDB(t)
			release := holdWriteLock(t, path)
			defer release()
			fast := fastWriter(t, path)

			start := time.Now()
			err := sqlite.WithTxRetry(context.Background(), fast, func(tx *sql.Tx) error {
				_, e := tx.Exec(`INSERT INTO t(id) VALUES (2)`)
				return e
			}, sqlite.WithRetryAttempts(n))
			elapsed := time.Since(start)
			if err == nil || !sqlite.IsBusy(err) {
				t.Fatalf("want busy error, got %v", err)
			}
			if elapsed >= 30*time.Millisecond {
				t.Fatalf("WithRetryAttempts(%d): want clamp to 1 attempt (no backoff wait), elapsed=%v", n, elapsed)
			}
		})
	}
}

func TestWithRetryInterval_NonPositiveIgnoredKeepsDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -5 * time.Millisecond} {
		t.Run(d.String(), func(t *testing.T) {
			_, path := txDB(t)
			release := holdWriteLock(t, path)
			defer release()
			fast := fastWriter(t, path)

			// Default interval is 50ms (defaultRetryConfig); with attempts=2 there is
			// exactly one backoff wait of ~50ms between attempts. If a non-positive
			// interval were NOT ignored, that wait would collapse to ~0 and this call
			// would return well under the threshold below.
			start := time.Now()
			err := sqlite.WithTxRetry(context.Background(), fast, func(tx *sql.Tx) error {
				_, e := tx.Exec(`INSERT INTO t(id) VALUES (2)`)
				return e
			}, sqlite.WithRetryAttempts(2), sqlite.WithRetryInterval(d))
			elapsed := time.Since(start)
			if err == nil || !sqlite.IsBusy(err) {
				t.Fatalf("want busy error, got %v", err)
			}
			if elapsed < 30*time.Millisecond {
				t.Fatalf("WithRetryInterval(%v): want default ~50ms backoff retained, elapsed=%v", d, elapsed)
			}
		})
	}
}

func TestWithTxRetry_GivesUpOnHeldWriteLock(t *testing.T) {
	_, path := txDB(t)
	// Hold the write lock from an independent connection with busy_timeout=0.
	blocker, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("blocker open: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	held, err := blocker.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("blocker begin: %v", err)
	}
	if _, err := held.Exec(`INSERT INTO t(id) VALUES (999)`); err != nil {
		t.Fatalf("blocker write: %v", err)
	}
	defer func() { _ = held.Rollback() }()

	// Force our writer to busy out fast: reopen with busy_timeout 0.
	cfg := sqlite.DefaultConfig()
	cfg.Path = path
	cfg.BusyTimeout = 0
	fast, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("fast open: %v", err)
	}
	defer sqlite.Close(fast, nil)

	err = sqlite.WithTxRetry(context.Background(), fast, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO t(id) VALUES (2)`)
		return e
	}, sqlite.WithRetryAttempts(3), sqlite.WithRetryInterval(time.Millisecond))
	if err == nil || !sqlite.IsBusy(err) {
		t.Fatalf("want busy error after retries, got %v", err)
	}
}

func TestWithTxRetry_HonorsContextCancel(t *testing.T) {
	db, _ := txDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sqlite.WithTxRetry(ctx, db, func(tx *sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("want error on cancelled ctx")
	}
}

func TestWithTxRetry_ContextCancelErrorIsSingleLineAndBusy(t *testing.T) {
	_, path := txDB(t)
	// Hold the write lock from an independent connection with busy_timeout=0.
	blocker, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("blocker open: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	held, err := blocker.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("blocker begin: %v", err)
	}
	if _, err := held.Exec(`INSERT INTO t(id) VALUES (999)`); err != nil {
		t.Fatalf("blocker write: %v", err)
	}
	defer func() { _ = held.Rollback() }()

	// Force our writer to busy out fast: reopen with busy_timeout 0.
	cfg := sqlite.DefaultConfig()
	cfg.Path = path
	cfg.BusyTimeout = 0
	fast, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("fast open: %v", err)
	}
	defer sqlite.Close(fast, nil)

	// First attempt busies out immediately; the 200ms backoff outlives the 20ms
	// cancel timer, so cancellation fires during the backoff wait. Use WithCancel +
	// AfterFunc (not WithTimeout) so ctx.Err() is context.Canceled, not
	// DeadlineExceeded.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(20*time.Millisecond, cancel)
	err = sqlite.WithTxRetry(ctx, fast, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO t(id) VALUES (2)`)
		return e
	}, sqlite.WithRetryAttempts(5), sqlite.WithRetryInterval(200*time.Millisecond))
	if err == nil {
		t.Fatal("want error on cancelled ctx during backoff")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("error message must be single line, got %q", err.Error())
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled cause, got %v", err)
	}
	if !sqlite.IsBusy(err) {
		t.Fatalf("want busy cause preserved, got %v", err)
	}
}
