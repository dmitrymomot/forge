package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/sqlite"
)

func TestWithMigrator_AppliesSQLiteMigrationsOnOpen(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_init.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nCREATE TABLE widgets (id INTEGER PRIMARY KEY);\n"),
		},
	}
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "m.db")

	db, err := sqlite.Open(context.Background(),
		sqlite.WithConfig(cfg),
		sqlite.WithMigrator(migration.New(fsys, migration.WithDialect(migration.SQLite))),
	)
	if err != nil {
		t.Fatalf("Open with migrator: %v", err)
	}
	defer sqlite.Close(db, nil)

	// Migration ran on the writer; the reader sees the schema.
	var name string
	if err := db.Reader().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='widgets'`).Scan(&name); err != nil {
		t.Fatalf("widgets table not visible to reader: %v", err)
	}
}

func TestWithMigrator_NilRejected(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	if _, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg), sqlite.WithMigrator(nil)); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestWithMigrator_FailureFailsOpen(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_bad.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nCREATE TABLE ;\n"), // invalid SQL
		},
	}
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "bad.db")
	if _, err := sqlite.Open(context.Background(),
		sqlite.WithConfig(cfg),
		sqlite.WithMigrator(migration.New(fsys, migration.WithDialect(migration.SQLite))),
	); err == nil {
		t.Fatal("bad migration must fail Open")
	}
}
