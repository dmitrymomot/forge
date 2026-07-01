package nullx_test

import (
	"encoding/json"
	"testing"

	"github.com/dmitrymomot/forge/nullx"
)

func BenchmarkMarshalJSON(b *testing.B) {
	n := nullx.Of("hello")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = json.Marshal(n)
	}
}

func BenchmarkGet(b *testing.B) {
	n := nullx.Of(42)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = n.Get()
	}
}
