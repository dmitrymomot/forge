package bufpool_test

import (
	"bytes"
	"testing"

	"github.com/dmitrymomot/forge/bufpool"
)

func BenchmarkGetPut(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		buf := bufpool.Get()
		buf.WriteString("hello world")
		bufpool.Put(buf)
	}
}

func BenchmarkDo(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = bufpool.Do(func(buf *bytes.Buffer) error {
			buf.WriteString("hello world")
			return nil
		})
	}
}

func TestGetPutZeroAlloc(t *testing.T) {
	// Warm the pool, then steady-state reuse must not allocate. If this proves
	// platform-flaky under GC pressure, relax to n > 1 (mirroring the id
	// package's <=1 alloc precedent) rather than deleting the guard.
	if n := testing.AllocsPerRun(1000, func() {
		buf := bufpool.Get()
		buf.WriteString("hello world")
		bufpool.Put(buf)
	}); n != 0 {
		t.Fatalf("Get/Put steady-state allocs = %v, want 0", n)
	}
}
