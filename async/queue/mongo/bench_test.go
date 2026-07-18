//go:build integration

package mongoqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

// Past-biased RunAt throughout so every pushed job is immediately claimable.
func benchJob(q string) queue.Job {
	return queue.Job{
		ID: id.NewUUID().String(), Queue: q, Type: "bench.mongo", Payload: []byte(`{"n":1}`),
		RunAt: time.Now().UTC().Add(-2 * time.Second), CreatedAt: time.Now().UTC(),
	}
}

func BenchmarkMongoPushClaimAck(b *testing.B) {
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

// BenchmarkMongoPushBatch measures the transactional multi-document push (the
// all-or-nothing path every batch > 1 takes).
func BenchmarkMongoPushBatch(b *testing.B) {
	broker := newBroker(b)
	ctx := context.Background()
	const batch = 100
	b.ReportAllocs()
	for b.Loop() {
		jobs := make([]queue.Job, batch)
		for i := range jobs {
			jobs[i] = benchJob("bulk")
		}
		if err := broker.Push(ctx, jobs...); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMongoClaimBatch measures a full claim round (candidate scan,
// guarded update, fetch) at a worker-sized batch, against a deep backlog.
func BenchmarkMongoClaimBatch(b *testing.B) {
	broker := newBroker(b)
	ctx := context.Background()

	// Seed enough backlog that Claim never runs dry inside the measured loop.
	const seed = 20000
	jobs := make([]queue.Job, seed)
	for i := range jobs {
		jobs[i] = benchJob("default")
	}
	if err := broker.Push(ctx, jobs...); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		got, err := broker.Claim(ctx, "default", 20, time.Minute)
		if err != nil {
			b.Fatal(err)
		}
		if len(got) == 0 {
			b.Fatal("backlog ran dry mid-benchmark")
		}
		for _, j := range got {
			if err := broker.Ack(ctx, j.ID, j.Token); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkMongoListDead(b *testing.B) {
	broker := newBroker(b)
	ctx := context.Background()

	// Seed once, before b.Loop(): push 5000 jobs in one batch, claim in
	// batches of 100 and Kill each with its token to build up the dead set
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
