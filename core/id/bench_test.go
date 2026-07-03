package id_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

func BenchmarkNewUUID(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = id.NewUUID()
	}
}

func BenchmarkNewULID(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = id.NewULID()
	}
}

func BenchmarkNewShort(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = id.NewShort()
	}
}

func BenchmarkGenerator_ULID(b *testing.B) {
	g := id.NewGenerator()
	b.ReportAllocs()
	for range b.N {
		_ = g.ULID()
	}
}

func BenchmarkGenerator_Monotonic(b *testing.B) {
	g := id.NewGenerator(id.WithClock(clock.NewMock(time.UnixMilli(1_700_000_000_000))), id.WithMonotonic())
	b.ReportAllocs()
	for range b.N {
		_ = g.ULID()
	}
}

func BenchmarkUUID_String(b *testing.B) {
	u := id.NewUUID()
	b.ReportAllocs()
	for range b.N {
		_ = u.String()
	}
}

func BenchmarkULID_String(b *testing.B) {
	u := id.NewULID()
	b.ReportAllocs()
	for range b.N {
		_ = u.String()
	}
}

func BenchmarkShort_String(b *testing.B) {
	s := id.NewShort()
	b.ReportAllocs()
	for range b.N {
		_ = s.String()
	}
}

func BenchmarkParseUUID(b *testing.B) {
	s := id.NewUUID().String()
	b.ReportAllocs()
	for range b.N {
		_, _ = id.ParseUUID(s)
	}
}

func BenchmarkParseULID(b *testing.B) {
	s := id.NewULID().String()
	b.ReportAllocs()
	for range b.N {
		_, _ = id.ParseULID(s)
	}
}

func BenchmarkParseShort(b *testing.B) {
	s := id.NewShort().String()
	b.ReportAllocs()
	for range b.N {
		_, _ = id.ParseShort(s)
	}
}

func BenchmarkUUID_Value(b *testing.B) {
	u := id.NewUUID()
	b.ReportAllocs()
	for range b.N {
		_, _ = u.Value()
	}
}

func BenchmarkUUID_Scan(b *testing.B) {
	s := id.NewUUID().String()
	var u id.UUID
	b.ReportAllocs()
	for range b.N {
		_ = u.Scan(s)
	}
}

func BenchmarkNewShortParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = id.NewShort()
		}
	})
}

func BenchmarkGeneratorMonotonicParallel(b *testing.B) {
	g := id.NewGenerator(id.WithClock(clock.NewMock(time.UnixMilli(1_700_000_000_000))), id.WithMonotonic())
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = g.Short()
		}
	})
}
