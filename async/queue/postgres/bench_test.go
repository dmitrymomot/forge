//go:build integration

package pgqueue_test

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
		ID: id.NewUUID().String(), Queue: q, Type: "bench.pg", Payload: []byte(`{"n":1}`),
		RunAt: time.Now().UTC().Add(-2 * time.Second), CreatedAt: time.Now().UTC(),
	}
}

func BenchmarkPgPushClaimAck(b *testing.B) {
	broker := newBroker(b, openPool(b))
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

func BenchmarkPgPushMany(b *testing.B) {
	for _, size := range []int{100, 10000} {
		name := "100"
		if size == 10000 {
			name = "10k"
		}
		b.Run(name, func(b *testing.B) {
			broker := newBroker(b, openPool(b))
			ctx := context.Background()
			jobs := make([]queue.Job, size)
			b.ReportAllocs()
			for b.Loop() {
				for i := range jobs {
					jobs[i] = benchJob("bulk")
				}
				if err := broker.Push(ctx, jobs...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
