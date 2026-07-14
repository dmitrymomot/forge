package clickhouse_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

func unreachableCfg() clickhouse.Config {
	c := clickhouse.DefaultConfig()
	c.DSN = "clickhouse://127.0.0.1:9/db" // port 9 (discard): refused fast
	c.RetryAttempts = 2
	c.RetryInterval = time.Millisecond
	c.DialTimeout = 200 * time.Millisecond
	return c
}

func TestOpen_OptionErrorShortCircuits(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.Open(context.Background(), clickhouse.WithLogger(nil))
	if !errors.Is(err, clickhouse.ErrInvalidConfig) {
		t.Fatalf("Open() = %v, want ErrInvalidConfig", err)
	}
}

func TestOpen_ValidateFailsBeforeIO(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.Open(context.Background(), clickhouse.WithConfig(clickhouse.Config{}))
	if !errors.Is(err, clickhouse.ErrInvalidConfig) {
		t.Fatalf("Open() = %v, want ErrInvalidConfig", err)
	}
}

func TestOpen_MalformedDSN(t *testing.T) {
	t.Parallel()
	cfg := clickhouse.DefaultConfig()
	cfg.DSN = "clickhouse://" // empty host
	_, err := clickhouse.Open(context.Background(), clickhouse.WithConfig(cfg))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("Open() = %v, want ErrConnect", err)
	}
}

func TestOpen_UnreachableExhaustsRetry(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.Open(context.Background(), clickhouse.WithConfig(unreachableCfg()))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("Open() = %v, want ErrConnect", err)
	}
}

func TestOpen_ContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := clickhouse.Open(ctx, clickhouse.WithConfig(unreachableCfg()))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("Open() = %v, want ErrConnect", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() = %v, want wrapped context.Canceled", err)
	}
}

func TestOpenDB_UnreachableExhaustsRetry(t *testing.T) {
	t.Parallel()
	_, err := clickhouse.OpenDB(context.Background(), clickhouse.WithConfig(unreachableCfg()))
	if !errors.Is(err, clickhouse.ErrConnect) {
		t.Fatalf("OpenDB() = %v, want ErrConnect", err)
	}
}
