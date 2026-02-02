package id_test

import (
	"testing"

	"github.com/dmitrymomot/forge/pkg/id"
)

func BenchmarkNewULID(b *testing.B) {
	for b.Loop() {
		_ = id.NewULID()
	}
}

func BenchmarkNewShortID(b *testing.B) {
	for b.Loop() {
		_ = id.NewShortID()
	}
}

func BenchmarkNewULIDParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = id.NewULID()
		}
	})
}

func BenchmarkNewShortIDParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = id.NewShortID()
		}
	})
}
