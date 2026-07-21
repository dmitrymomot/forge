//go:build integration

package pgbus_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/realtime/fanout/pgbus"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

func BenchmarkPublish(b *testing.B) {
	pool, err := pgxpool.New(context.Background(), pgtest.DSN(b))
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	bus, err := pgbus.New(pool, pgbus.WithChannel("fanout_bench"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	payload := []byte(`{"text":"hi"}`)
	b.ReportAllocs()
	for b.Loop() {
		if err := bus.Publish(ctx, "bench", payload); err != nil {
			b.Fatal(err)
		}
	}
}
