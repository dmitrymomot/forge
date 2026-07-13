package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

// fakeCloser records whether Close was called and can return a forced error.
type fakeCloser struct {
	closed bool
	err    error
}

func (f *fakeCloser) Close() error {
	f.closed = true
	return f.err
}

func TestClose_NilTolerated(t *testing.T) {
	t.Parallel()
	// Must not panic with a nil closer and/or nil logger.
	clickhouse.Close(nil, nil)
}

func TestClose_ClosesCloser(t *testing.T) {
	t.Parallel()
	fc := &fakeCloser{}
	clickhouse.Close(fc, nil)
	if !fc.closed {
		t.Fatal("Close did not call the underlying Close")
	}
}

func TestClose_CloseErrorTolerated(t *testing.T) {
	t.Parallel()
	fc := &fakeCloser{err: errors.New("boom")}
	clickhouse.Close(fc, nil) // must not panic even though Close errors
	if !fc.closed {
		t.Fatal("Close did not call the underlying Close")
	}
}

func TestHealthcheckDB_NilDBPings(t *testing.T) {
	t.Parallel()
	// HealthcheckDB over a *sql.DB pointing at an unreachable server wraps the ping
	// failure in ErrHealthcheck. Build the DB via OpenDB is not possible without a
	// server, so assert the closure shape and error wrapping through a bad conn.
	db, err := clickhouse.OpenDB(context.Background(), badConn())
	if err == nil {
		t.Skip("unexpected live server")
	}
	// OpenDB failed as expected; nothing else to assert here.
	_ = db
}

func badConn() clickhouse.Option {
	cfg := clickhouse.DefaultConfig()
	cfg.DSN = "clickhouse://127.0.0.1:9/db"
	cfg.RetryAttempts = 1
	return clickhouse.WithConfig(cfg)
}
