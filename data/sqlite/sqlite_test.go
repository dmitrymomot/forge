package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func openFile(t *testing.T, opts ...sqlite.Option) *sqlite.DB {
	t.Helper()
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "app.db")
	all := append([]sqlite.Option{sqlite.WithConfig(cfg)}, opts...)
	db, err := sqlite.Open(context.Background(), all...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { sqlite.Close(db, nil) })
	return db
}

func TestOpen_RequiresPath(t *testing.T) {
	if _, err := sqlite.Open(context.Background()); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestOpen_WALAndForeignKeysEnabled(t *testing.T) {
	db := openFile(t)
	var mode string
	if err := db.Writer().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal_mode=%q, want wal", mode)
	}
	var fk int
	if err := db.Reader().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys=%d, want 1", fk)
	}
}

func TestReader_IsQueryOnly(t *testing.T) {
	db := openFile(t)
	if _, err := db.Reader().ExecContext(context.Background(), `CREATE TABLE x(id INTEGER)`); err == nil {
		t.Fatal("write through reader must fail (query_only)")
	}
}

func TestDB_ExecRoutesToWriterQueryToReader(t *testing.T) {
	db := openFile(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t(id) VALUES (42)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var id int
	if err := db.QueryRowContext(ctx, `SELECT id FROM t`).Scan(&id); err != nil {
		t.Fatalf("read via reader: %v", err)
	}
	if id != 42 {
		t.Fatalf("id=%d, want 42", id)
	}
}

func TestConcurrentReadWrite_NoBusy(t *testing.T) {
	db := openFile(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for range 8 {
		wg.Go(func() {
			for range 200 {
				var n int
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&n); err != nil {
					errCh <- err
					return
				}
			}
		})
	}
	wg.Go(func() {
		for i := range 500 {
			if _, err := db.ExecContext(ctx, `INSERT INTO t(id) VALUES (?)`, i); err != nil {
				errCh <- err
				return
			}
		}
	})
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent op failed (busy=%v): %v", sqlite.IsBusy(err), err)
	}
}

func TestMemory_IsolatedBetweenOpens(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = ":memory:"
	ctx := context.Background()
	db1, err := sqlite.Open(ctx, sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer sqlite.Close(db1, nil)
	db2, err := sqlite.Open(ctx, sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer sqlite.Close(db2, nil)

	if _, err := db1.ExecContext(ctx, `CREATE TABLE only1(id INTEGER)`); err != nil {
		t.Fatalf("create in db1: %v", err)
	}
	// Reader in the SAME instance sees it (shared cache).
	if _, err := db1.Reader().ExecContext(ctx, `SELECT 1 FROM only1`); err != nil {
		t.Fatalf("db1 reader cannot see table: %v", err)
	}
	// A different instance must NOT.
	if _, err := db2.Reader().ExecContext(ctx, `SELECT 1 FROM only1`); err == nil {
		t.Fatal("db2 must not see db1's table")
	}
}

func TestWithPragma_Overrides(t *testing.T) {
	// The default cache_size is -16000 (see config.go). The override value below
	// ("-1") is lexicographically LESS than "-16000" (')' < '6' at the first
	// differing byte), so modernc's _pragma re-sort — busy_timeout first, then
	// ascending by "name(value)" — would apply the Config-derived default AFTER
	// this override if both reached the driver as separate params, silently
	// discarding it. That makes this case a real regression guard for the
	// dedupe-by-name fix in buildDSN, unlike an override that happens to sort
	// after the default (which would pass even without dedupe).
	db := openFile(t, sqlite.WithPragma("cache_size", "-1"))
	var n int
	if err := db.Writer().QueryRow(`PRAGMA cache_size`).Scan(&n); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if n != -1 {
		t.Fatalf("cache_size=%d, want -1", n)
	}
}

func TestOpen_PingFailureWrapsErrConnect(t *testing.T) {
	// A directory cannot be opened as a SQLite DB file, so the writer's PingContext
	// fails deterministically and Open must clean up both pools and wrap ErrConnect.
	cfg := sqlite.DefaultConfig()
	cfg.Path = t.TempDir()
	_, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if !errors.Is(err, sqlite.ErrConnect) {
		t.Fatalf("want ErrConnect, got %v", err)
	}
}

func TestWithLogger_NilRejected(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	if _, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg), sqlite.WithLogger(nil)); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}
