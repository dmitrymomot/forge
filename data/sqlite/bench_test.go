package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func benchDB(b *testing.B) *sqlite.DB {
	b.Helper()
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(b.TempDir(), "bench.db")
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { sqlite.Close(db, nil) })
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		b.Fatalf("create: %v", err)
	}
	return db
}

func BenchmarkInsert(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := db.ExecContext(ctx, `INSERT INTO t(v) VALUES (?)`, "x"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSelect(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO t(id, v) VALUES (1, 'x')`); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		var v string
		if err := db.QueryRowContext(ctx, `SELECT v FROM t WHERE id=1`).Scan(&v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWithTx(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := sqlite.WithTx(ctx, db, func(tx *sql.Tx) error {
			_, e := tx.Exec(`INSERT INTO t(v) VALUES (?)`, "x")
			return e
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentReadWrite(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO t(id, v) VALUES (1, 'x')`); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var v string
			if err := db.QueryRowContext(ctx, `SELECT v FROM t WHERE id=1`).Scan(&v); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
