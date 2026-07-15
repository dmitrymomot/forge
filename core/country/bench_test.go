package country_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/country"
)

func BenchmarkByAlpha2(b *testing.B) {
	for b.Loop() {
		_, _ = country.ByAlpha2("US")
	}
}

func BenchmarkByDialCode(b *testing.B) {
	for b.Loop() {
		_ = country.ByDialCode("1")
	}
}

func BenchmarkAll(b *testing.B) {
	for b.Loop() {
		_ = country.All()
	}
}

func BenchmarkNewSetFromCodes(b *testing.B) {
	for b.Loop() {
		_, _ = country.NewSetFromCodes("US", "GB", "DE", "FR")
	}
}
