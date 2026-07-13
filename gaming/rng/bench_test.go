package rng_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func benchStream(b *testing.B) *rng.Stream {
	b.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	s, err := rng.New(seed, "bench", 0)
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func BenchmarkStreamUint64(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.Uint64()
	}
}

func BenchmarkStreamIntN(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.IntN(100)
	}
}

func BenchmarkStreamFloat64(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.Float64()
	}
}

func BenchmarkStreamInts5(b *testing.B) {
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = s.Ints(5, 100)
	}
}

func BenchmarkTablePick(b *testing.B) {
	table, err := rng.NewTable(testEntries())
	if err != nil {
		b.Fatal(err)
	}
	s := benchStream(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = table.Pick(s)
	}
}

func BenchmarkTablePickWithPity(b *testing.B) {
	table, err := rng.NewTable(testEntries(), rng.WithPity(90, "legendary"))
	if err != nil {
		b.Fatal(err)
	}
	s := benchStream(b)
	b.ReportAllocs()
	var misses uint64
	for b.Loop() {
		_, misses = table.PickWithPity(s, misses)
	}
}

func BenchmarkManagerPlay(b *testing.B) {
	m, err := rng.NewManager(rng.NewMemoryStore())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := m.Play(ctx, "bench"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := m.Play(ctx, "bench"); err != nil {
			b.Fatal(err)
		}
	}
}
