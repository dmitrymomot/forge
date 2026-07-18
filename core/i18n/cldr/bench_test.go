package cldr_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/i18n"
	"github.com/dmitrymomot/forge/core/i18n/cldr"
)

// Sinks every benchmark writes its result into, so the compiler cannot prove
// the call's result is unused and dead-code-eliminate it.
var (
	sinkCategory i18n.PluralCategory
	sinkRule     i18n.PluralRule
	sinkOK       bool
)

// BenchmarkRuleEn covers the simplest rule shape: one/other with no mod-10 or
// mod-100 windows.
func BenchmarkRuleEn(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkCategory = cldr.En(5)
	}
}

// BenchmarkRuleUk covers the East Slavic shape: mod-10/mod-100 windows plus
// the family-bucket exception at n%100 in 11..14.
func BenchmarkRuleUk(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkCategory = cldr.Uk(21)
	}
}

// BenchmarkRuleAr covers the most expensive shape shipped: Arabic's six
// categories with the widest set of range checks.
func BenchmarkRuleAr(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkCategory = cldr.Ar(103)
	}
}

// BenchmarkPluralFor measures the by-language-tag lookup, including the
// regional-tag reduction ("pt-BR" -> "pt").
func BenchmarkPluralFor(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkRule, sinkOK = cldr.PluralFor("pt-BR")
	}
}
