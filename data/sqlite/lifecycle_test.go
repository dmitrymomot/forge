package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func TestClose_NilTolerant(t *testing.T) {
	sqlite.Close(nil, nil) // must not panic
}

func TestHealthcheck_OKThenFailAfterClose(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "app.db")
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	check := sqlite.Healthcheck(db)
	if err := check(context.Background()); err != nil {
		t.Fatalf("healthy check failed: %v", err)
	}
	sqlite.Close(db, nil)
	if err := check(context.Background()); !errors.Is(err, sqlite.ErrHealthcheck) {
		t.Fatalf("want ErrHealthcheck after close, got %v", err)
	}
}
