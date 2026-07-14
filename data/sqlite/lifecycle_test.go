package sqlite_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func TestClose_NilTolerant(t *testing.T) {
	sqlite.Close(nil, nil) // must not panic
}

// captureHandler is a minimal slog.Handler that records each record's Message.
type captureHandler struct{ msgs *[]string }

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	*h.msgs = append(*h.msgs, r.Message)
	return nil
}

func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h captureHandler) WithGroup(string) slog.Handler { return h }

func TestClose_UsesConfiguredLoggerWhenArgNil(t *testing.T) {
	var msgs []string
	logger := slog.New(captureHandler{msgs: &msgs})

	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "app.db")
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg), sqlite.WithLogger(logger))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	sqlite.Close(db, nil) // nil arg must fall back to the WithLogger logger

	if !slices.Contains(msgs, "closing sqlite database") {
		t.Fatalf("configured logger did not receive the close line; got %v", msgs)
	}
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
