package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func classifyDB(t *testing.T) *sqlite.DB {
	t.Helper()
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "c.db")
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlite.Close(db, nil) })
	return db
}

func TestIsUniqueViolation(t *testing.T) {
	db := classifyDB(t)
	ctx := context.Background()
	mustExec(t, db, `CREATE TABLE u(id INTEGER PRIMARY KEY, email TEXT UNIQUE)`)
	mustExec(t, db, `INSERT INTO u(id, email) VALUES (1, 'a@x')`)
	_, err := db.ExecContext(ctx, `INSERT INTO u(id, email) VALUES (2, 'a@x')`)
	if !sqlite.IsUniqueViolation(err) {
		t.Fatalf("want unique violation, got %v", err)
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	db := classifyDB(t)
	ctx := context.Background()
	mustExec(t, db, `CREATE TABLE parent(id INTEGER PRIMARY KEY)`)
	mustExec(t, db, `CREATE TABLE child(id INTEGER PRIMARY KEY, pid INTEGER REFERENCES parent(id))`)
	_, err := db.ExecContext(ctx, `INSERT INTO child(id, pid) VALUES (1, 999)`)
	if !sqlite.IsForeignKeyViolation(err) {
		t.Fatalf("want FK violation, got %v", err)
	}
}

func TestIsNotFound(t *testing.T) {
	db := classifyDB(t)
	mustExec(t, db, `CREATE TABLE t(id INTEGER PRIMARY KEY)`)
	var id int
	err := db.QueryRowContext(context.Background(), `SELECT id FROM t WHERE id=1`).Scan(&id)
	if !sqlite.IsNotFound(err) || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
}

func TestIsBusy_FalseForNonSQLiteErrors(t *testing.T) {
	if sqlite.IsBusy(nil) || sqlite.IsBusy(errors.New("boom")) {
		t.Fatal("IsBusy must be false for nil and non-sqlite errors")
	}
}

func mustExec(t *testing.T, db *sqlite.DB, q string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}
