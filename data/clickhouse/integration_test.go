package clickhouse_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

// TestIntegration exercises a real ClickHouse when CLICKHOUSE_DSN is set (e.g. an
// ephemeral clickhouse/clickhouse-server container in CI); it is skipped otherwise.
func TestIntegration(t *testing.T) {
	dsn := os.Getenv("CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("set CLICKHOUSE_DSN to run the live integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := clickhouse.DefaultConfig()
	cfg.DSN = dsn

	// Native path: Open + PrepareBatch round-trip.
	conn, err := clickhouse.Open(ctx, clickhouse.WithConfig(cfg))
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer clickhouse.Close(conn, nil)

	if err := clickhouse.Healthcheck(conn)(ctx); err != nil {
		t.Fatalf("Healthcheck() = %v", err)
	}

	const table = "forge_clickhouse_it"
	if err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (id UInt64) ENGINE = Memory"); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) }()

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO "+table+" (id) VALUES")
	if err != nil {
		t.Fatalf("PrepareBatch: %v", err)
	}
	if err := batch.Append(uint64(7)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := batch.Send(); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var got uint64
	if err := conn.QueryRow(ctx, "SELECT id FROM "+table+" LIMIT 1").Scan(&got); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if got != 7 {
		t.Fatalf("round-trip id = %d, want 7", got)
	}

	// Classify: a query against a missing table is UNKNOWN_TABLE (60).
	err = conn.Exec(ctx, "SELECT 1 FROM forge_clickhouse_missing_9x8y7z")
	if !clickhouse.IsTableNotFound(err) {
		t.Fatalf("IsTableNotFound(%v) = false, want true", err)
	}

	// database/sql path: OpenDB + HealthcheckDB.
	db, err := clickhouse.OpenDB(ctx, clickhouse.WithConfig(cfg))
	if err != nil {
		t.Fatalf("OpenDB() = %v", err)
	}
	defer clickhouse.Close(db, nil)
	if err := clickhouse.HealthcheckDB(db)(ctx); err != nil {
		t.Fatalf("HealthcheckDB() = %v", err)
	}
}
