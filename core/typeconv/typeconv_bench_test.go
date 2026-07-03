package typeconv_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/typeconv"
)

func BenchmarkParseInt(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = typeconv.ParseInt[int]("2147483")
	}
}

func BenchmarkParseGeneric(b *testing.B) {
	// Parse boxes the result into any: small ints avoid the alloc, larger
	// values box onto the heap — prefer the typed helpers on hot paths.
	b.ReportAllocs()
	for b.Loop() {
		_, _ = typeconv.Parse[int]("2147483")
	}
}

func BenchmarkFormat(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = typeconv.Format(2147483)
	}
}

func BenchmarkParseSlice(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = typeconv.ParseSlice[int]("1,2,3,4,5", ",")
	}
}

func TestParseIntZeroAlloc(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		_, _ = typeconv.ParseInt[int]("2147483")
	}); n != 0 {
		t.Fatalf("ParseInt allocs = %v, want 0", n)
	}
}
