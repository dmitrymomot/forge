//go:build integration

package redisqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

// Past-biased RunAt throughout: visibility is decided by the database clock,
// which can lag the test process on a Docker VM (see brokertest.dueNow).
func benchJob(q string) queue.Job {
	return queue.Job{
		ID: id.NewUUID().String(), Queue: q, Type: "bench.redis", Payload: []byte(`{"n":1}`),
		RunAt: time.Now().UTC().Add(-2 * time.Second), CreatedAt: time.Now().UTC(),
	}
}

func BenchmarkRedisPushClaimAck(b *testing.B) {
	broker := newBroker(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := broker.Push(ctx, benchJob("default")); err != nil {
			b.Fatal(err)
		}
		jobs, err := broker.Claim(ctx, "default", 1, time.Minute)
		if err != nil || len(jobs) != 1 {
			b.Fatalf("claim: %v (%d jobs)", err, len(jobs))
		}
		if err := broker.Ack(ctx, jobs[0].ID, jobs[0].Token); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRedisListDead(b *testing.B) {
	broker := newBroker(b)
	ctx := context.Background()

	// Seed once, before b.Loop(): push 5000 jobs in one batch, claim in
	// batches of 100 and Kill each with its token to build up the dead index
	// that ListDead scans. Seeding cost must stay out of the measured loop.
	const total = 5000
	const batch = 100
	jobs := make([]queue.Job, total)
	for i := range jobs {
		jobs[i] = benchJob("default")
	}
	if err := broker.Push(ctx, jobs...); err != nil {
		b.Fatal(err)
	}
	for claimed := 0; claimed < total; claimed += batch {
		got, err := broker.Claim(ctx, "default", batch, time.Minute)
		if err != nil || len(got) != batch {
			b.Fatalf("claim: %v (%d jobs)", err, len(got))
		}
		for _, j := range got {
			if err := broker.Kill(ctx, j.ID, j.Token, "bench"); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := broker.ListDead(ctx, "default", 50); err != nil {
			b.Fatal(err)
		}
	}
}
