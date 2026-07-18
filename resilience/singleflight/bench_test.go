package singleflight_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/resilience/singleflight"
)

// BenchmarkDo measures the solo-leader path: each call registers, runs fn
// inline, and deregisters — no joiners.
func BenchmarkDo(b *testing.B) {
	var g singleflight.Group[int]
	ctx := context.Background()
	fn := func(context.Context) (int, error) { return 42, nil }
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := g.Do(ctx, "k", fn); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDoDetached measures the solo-initiator path: each call registers,
// spawns the leader goroutine, and waits on the flight's done channel.
func BenchmarkDoDetached(b *testing.B) {
	var g singleflight.Group[int]
	ctx := context.Background()
	fn := func(context.Context) (int, error) { return 42, nil }
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := g.DoDetached(ctx, "k", fn); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDoContended measures Do under parallel callers on one key, where
// registration contends on the Group mutex and most callers join as waiters.
func BenchmarkDoContended(b *testing.B) {
	var g singleflight.Group[int]
	ctx := context.Background()
	fn := func(context.Context) (int, error) { return 42, nil }
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, _, err := g.Do(ctx, "k", fn); err != nil {
				b.Fatal(err)
			}
		}
	})
}
