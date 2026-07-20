package outbox_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/outbox"
	"github.com/dmitrymomot/forge/async/queue"
)

func benchJobs(n int, prefix string) []queue.Job {
	jobs := make([]queue.Job, n)
	for i := range jobs {
		jobs[i] = makeJob(prefix+strconv.Itoa(i), testEpoch.Add(time.Duration(i)*time.Millisecond))
	}
	return jobs
}

func BenchmarkMemoryStore_Add(b *testing.B) {
	ctx := context.Background()
	s := outbox.NewMemoryStore()
	jobs := benchJobs(100, "add")
	b.ReportAllocs()
	for b.Loop() {
		if err := s.Add(ctx, nil, jobs...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryStore_ClaimDelete(b *testing.B) {
	ctx := context.Background()
	s := outbox.NewMemoryStore()
	jobs := benchJobs(100, "claim")
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := s.Add(ctx, nil, jobs...); err != nil {
			b.Fatal(err)
		}
		got, err := s.Claim(ctx, 100, time.Minute)
		if err != nil {
			b.Fatal(err)
		}
		if len(got) != 100 {
			b.Fatalf("claimed %d, want 100", len(got))
		}
		if err := s.Delete(ctx, ids...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBroker_PushTx(b *testing.B) {
	ctx := context.Background()
	s := outbox.NewMemoryStore()
	br := outbox.Wrap(s, queue.NewMemoryBroker())
	jobs := benchJobs(100, "tx")
	b.ReportAllocs()
	for b.Loop() {
		if err := br.PushTx(ctx, nil, jobs...); err != nil {
			b.Fatal(err)
		}
	}
}
