//go:build integration

package pgqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

func BenchmarkPgPushClaimAck(b *testing.B) {
	broker := newBroker(b, openPool(b)) // helpers take testing.TB; skips without env
	ctx := context.Background()
	c := queue.NewClient(broker)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("bench.pg")
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := queue.Push(ctx, c, kind, struct {
			N int `json:"n"`
		}{N: i}); err != nil {
			b.Fatal(err)
		}
		jobs, err := broker.Claim(ctx, "default", 1, time.Minute)
		if err != nil || len(jobs) != 1 {
			b.Fatalf("claim: %v (%d jobs)", err, len(jobs))
		}
		if err := broker.Ack(ctx, jobs[0].ID); err != nil {
			b.Fatal(err)
		}
	}
}
