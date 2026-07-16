package queue_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

type benchPayload struct {
	N int `json:"n"`
}

var kindBench = queue.NewKind[benchPayload]("bench.job")

func BenchmarkPush_Memory(b *testing.B) {
	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := queue.Push(ctx, c, kindBench, benchPayload{N: i}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPushMany_Memory(b *testing.B) {
	for _, size := range []int{100, 10000} {
		b.Run(sizeName(size), func(b *testing.B) {
			broker := queue.NewMemoryBroker()
			c := queue.NewClient(broker)
			ctx := context.Background()
			payloads := make([]benchPayload, size)
			for i := range payloads {
				payloads[i] = benchPayload{N: i}
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := queue.PushMany(ctx, c, kindBench, payloads); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// sizeName labels a benchmark size for the {100, 10000} set used here and in
// the postgres/redis bench files. Keep the three files in agreement on
// naming since the results get diffed against each other.
func sizeName(n int) string {
	switch n {
	case 10000:
		return "10k"
	default:
		return "100"
	}
}

func BenchmarkClaimBatch_Memory(b *testing.B) {
	ctx := context.Background()
	broker := queue.NewMemoryBroker()
	c := queue.NewClient(broker)
	for i := range 10_000 {
		if err := queue.Push(ctx, c, kindBench, benchPayload{N: i}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		jobs, err := broker.Claim(ctx, "default", 100, time.Minute)
		if err != nil {
			b.Fatal(err)
		}
		for _, j := range jobs {
			if err := broker.Nack(ctx, j.ID, j.Token, time.Now(), ""); err != nil { // recycle for the next iteration
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkClaimBatch_Memory_MultiQueue demonstrates the per-queue-bucket
// rework's actual payoff (issue #12): Claim cost should track the claimed
// queue's live set, not the whole broker's. BenchmarkClaimBatch_Memory above
// cannot show this — it only ever creates one queue, so the claimed bucket IS
// the whole broker there, both before and after the rework.
//
// Shape: the claimed queue ("default") always holds a fixed 100 live jobs,
// recycled via Nack exactly like BenchmarkClaimBatch_Memory. A second "noise"
// queue holds 1k/10k/100k live jobs that are NEVER claimed from — it exists
// purely to inflate the broker's total live-job count. Under per-queue
// buckets, Claim("default", ...) never touches the noise bucket, so ns/op
// should stay flat across the three sub-benchmarks despite a 100x change in
// noise size. Under the old whole-broker scan (issue #12), Claim scanned
// every live job regardless of queue, so ns/op would climb roughly linearly
// with noise size instead.
//
// A true old-vs-new comparison isn't possible here: the pre-rework broker is
// nine commits back and the Broker interface has changed shape since. So this
// benchmark is self-demonstrating instead — flatness across the sub-bench
// sizes (or its absence) is the evidence, not a diff against a baseline file.
func BenchmarkClaimBatch_Memory_MultiQueue(b *testing.B) {
	const claimedSize = 100
	noiseSizes := []struct {
		n    int
		name string
	}{
		{1_000, "1k"},
		{10_000, "10k"},
		{100_000, "100k"},
	}
	for _, ns := range noiseSizes {
		b.Run("noise="+ns.name, func(b *testing.B) {
			ctx := context.Background()
			broker := queue.NewMemoryBroker()
			c := queue.NewClient(broker)
			for i := range claimedSize {
				if err := queue.Push(ctx, c, kindBench, benchPayload{N: i}); err != nil {
					b.Fatal(err)
				}
			}
			noisePayloads := make([]benchPayload, ns.n)
			for i := range noisePayloads {
				noisePayloads[i] = benchPayload{N: i}
			}
			if err := queue.PushMany(ctx, c, kindBench, noisePayloads, queue.WithQueue("noise")); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				jobs, err := broker.Claim(ctx, "default", claimedSize, time.Minute)
				if err != nil {
					b.Fatal(err)
				}
				for _, j := range jobs {
					if err := broker.Nack(ctx, j.ID, j.Token, time.Now(), ""); err != nil { // recycle for the next iteration
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkEndToEnd_Memory MUST be run with -benchtime=5000x: with time-based
// benchtime the push loop outruns the drain workers and the drain wait after
// the loop dominates (see the baseline file's note).
func BenchmarkEndToEnd_Memory(b *testing.B) {
	ctx := context.Background()
	broker := queue.NewMemoryBroker()
	cfg := queue.DefaultConfig()
	cfg.PollInterval = time.Millisecond
	cfg.Concurrency = 16
	svc, err := queue.NewService(broker, queue.WithConfig(cfg))
	if err != nil {
		b.Fatal(err)
	}
	var processed atomic.Int64
	queue.Register(svc, kindBench, func(context.Context, benchPayload) error {
		processed.Add(1)
		return nil
	})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = svc.Run(runCtx) }()

	c := queue.NewClient(broker)
	b.ReportAllocs()
	n := 0
	for b.Loop() {
		n++
		if err := queue.Push(ctx, c, kindBench, benchPayload{N: n}); err != nil {
			b.Fatal(err)
		}
	}
	for processed.Load() < int64(n) {
		time.Sleep(time.Millisecond)
	}
}
