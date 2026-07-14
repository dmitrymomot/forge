package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
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

func TestWithTxRetry_GivesUpOnHeldWriteLock(t *testing.T) {
	_, path := txDB(t)
	// Hold the write lock from an independent connection with busy_timeout=0.
	blocker, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("blocker open: %v", err)
	}
	defer blocker.Close()
	held, err := blocker.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("blocker begin: %v", err)
	}
	if _, err := held.Exec(`INSERT INTO t(id) VALUES (999)`); err != nil {
		t.Fatalf("blocker write: %v", err)
	}
	defer held.Rollback()

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
