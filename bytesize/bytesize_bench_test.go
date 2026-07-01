package bytesize_test

import (
	"testing"

	"github.com/dmitrymomot/forge/bytesize"
)

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = bytesize.Parse("1.5GiB")
	}
}

func BenchmarkString(b *testing.B) {
	v := 10 * bytesize.MiB
	b.ReportAllocs()
	for b.Loop() {
		_ = v.String()
	}
}
