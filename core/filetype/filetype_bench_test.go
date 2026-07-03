package filetype_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/dmitrymomot/forge/core/filetype"
)

var (
	benchPNGHead   = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	benchPDFHead   = []byte("%PDF-1.7\n")
	benchPlainHead = []byte("just some plain text content")
)

func BenchmarkDetectPNG(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = filetype.Detect(benchPNGHead)
	}
}

func BenchmarkDetectPDF(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = filetype.Detect(benchPDFHead)
	}
}

func BenchmarkDetectPlain(b *testing.B) {
	// No signature match, so this exercises the net/http fallback path.
	b.ReportAllocs()
	for b.Loop() {
		_, _ = filetype.Detect(benchPlainHead)
	}
}

func BenchmarkIs(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = filetype.Is(benchPNGHead, "image/png")
	}
}

func BenchmarkDetectReader(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		r := bytes.NewReader(benchPNGHead)
		typ, rest, err := filetype.DetectReader(r)
		_, _ = typ, err
		_, _ = io.Copy(io.Discard, rest)
	}
}
