package iox_test

import (
	"io"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/iox"
)

func BenchmarkLimitReaderRead(b *testing.B) {
	src := strings.NewReader(strings.Repeat("x", 4096))
	buf := make([]byte, 4096)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = src.Seek(0, io.SeekStart)
		r := iox.LimitReader(src, 4096)
		_, _ = io.ReadFull(r, buf)
	}
}

func BenchmarkCountingWriter(b *testing.B) {
	cw := iox.NewCountingWriter(io.Discard)
	p := []byte("hello world")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = cw.Write(p)
	}
}

func TestCountingWriterZeroAlloc(t *testing.T) {
	cw := iox.NewCountingWriter(io.Discard)
	p := []byte("hello world")
	if n := testing.AllocsPerRun(100, func() {
		_, _ = cw.Write(p)
	}); n != 0 {
		t.Fatalf("CountingWriter.Write allocs = %v, want 0", n)
	}
}
