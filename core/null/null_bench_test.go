package null_test

import (
	"encoding/json"
	"testing"

	"github.com/dmitrymomot/forge/core/null"
)

func BenchmarkMarshalJSON(b *testing.B) {
	n := null.Of("hello")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = json.Marshal(n)
	}
}

func BenchmarkGet(b *testing.B) {
	n := null.Of(42)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = n.Get()
	}
}
