package migration_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"

	"github.com/dmitrymomot/forge/data/migration"
)

func TestNew_SQLiteDialect_AppliesMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_init.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nCREATE TABLE probe (id INTEGER PRIMARY KEY);\n"),
		},
	}
	dsn := "file:" + filepath.Join(t.TempDir(), "m.db") + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	m := migration.New(fsys, migration.WithDialect(migration.SQLite))
	if err := m.Up(context.Background(), db); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='probe'`).Scan(&name)
	if err != nil {
		t.Fatalf("probe table not created: %v", err)
	}
	if name != "probe" {
		t.Fatalf("got table %q, want probe", name)
	}
}
