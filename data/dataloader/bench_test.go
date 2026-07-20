package dataloader_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/dataloader"
)

func newBenchLoader(opts ...dataloader.Option) *dataloader.Loader[string, string] {
	return dataloader.New(func(_ context.Context, keys []string) (map[string]string, error) {
		out := make(map[string]string, len(keys))
		for _, k := range keys {
			out[k] = k
		}
		return out, nil
	}, opts...)
}

// BenchmarkLoadCached is the request hot path: every key already resolved.
func BenchmarkLoadCached(b *testing.B) {
	l := newBenchLoader(dataloader.WithMaxBatchSize(1))
	ctx := b.Context()
	if _, err := l.Load(ctx, "k"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := l.Load(ctx, "k"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoadCachedParallel(b *testing.B) {
	l := newBenchLoader(dataloader.WithMaxBatchSize(1))
	if _, err := l.Load(b.Context(), "k"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			if _, err := l.Load(ctx, "k"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkLoadManyCold measures batching 100 fresh keys into one fetch.
func BenchmarkLoadManyCold(b *testing.B) {
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		l := newBenchLoader(dataloader.WithWait(time.Hour), dataloader.WithMaxBatchSize(100))
		if _, err := l.LoadMany(ctx, keys); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadManyWarm measures the all-cached bulk path.
func BenchmarkLoadManyWarm(b *testing.B) {
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	ctx := b.Context()
	l := newBenchLoader(dataloader.WithWait(time.Hour), dataloader.WithMaxBatchSize(100))
	if _, err := l.LoadMany(ctx, keys); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := l.LoadMany(ctx, keys); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadColdSolo measures a full miss->batch-of-one->resolve cycle.
func BenchmarkLoadColdSolo(b *testing.B) {
	ctx := b.Context()
	l := newBenchLoader(dataloader.WithMaxBatchSize(1))
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		if _, err := l.Load(ctx, strconv.Itoa(i)); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkPrime(b *testing.B) {
	l := newBenchLoader()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		l.Prime(strconv.Itoa(i), "v")
		i++
	}
}
