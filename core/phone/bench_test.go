package phone_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/country"
	"github.com/dmitrymomot/forge/core/phone"
)

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		_, _ = phone.Parse("+1 (415) 555-2671")
	}
}

func BenchmarkParseRegion(b *testing.B) {
	for b.Loop() {
		_, _ = phone.ParseRegion("07911 123456", "GB")
	}
}

func BenchmarkParserGate(b *testing.B) {
	set := country.NewSet(country.US, country.GB, country.DE)
	p, err := phone.New(phone.WithAllowedCountries(set))
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Parse("+44 20 7946 0018")
	}
}
