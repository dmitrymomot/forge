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
