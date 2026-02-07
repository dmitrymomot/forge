package i18n_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

func TestDefaultPluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralZero},
		{1, i18n.PluralOne},
		{2, i18n.PluralFew},
		{3, i18n.PluralFew},
		{4, i18n.PluralFew},
		{5, i18n.PluralMany},
		{10, i18n.PluralMany},
		{19, i18n.PluralMany},
		{20, i18n.PluralOther},
		{100, i18n.PluralOther},
		{-1, i18n.PluralOne},
		{-5, i18n.PluralMany},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.DefaultPluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestEnglishPluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralZero},
		{1, i18n.PluralOne},
		{2, i18n.PluralOther},
		{5, i18n.PluralOther},
		{10, i18n.PluralOther},
		{100, i18n.PluralOther},
		{1000, i18n.PluralOther},
		{-1, i18n.PluralOne},
		{-2, i18n.PluralOther},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.EnglishPluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSlavicPluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralZero},
		{1, i18n.PluralOne},
		{2, i18n.PluralFew},
		{3, i18n.PluralFew},
		{4, i18n.PluralFew},
		{22, i18n.PluralFew},
		{23, i18n.PluralFew},
		{24, i18n.PluralFew},
		{102, i18n.PluralFew},
		{5, i18n.PluralMany},
		{10, i18n.PluralMany},
		{11, i18n.PluralMany},
		{12, i18n.PluralMany},
		{13, i18n.PluralMany},
		{14, i18n.PluralMany},
		{15, i18n.PluralMany},
		{20, i18n.PluralMany},
		{21, i18n.PluralMany},
		{25, i18n.PluralMany},
		{100, i18n.PluralMany},
		{112, i18n.PluralMany},
		{-1, i18n.PluralOne},
		{-2, i18n.PluralFew},
		{-5, i18n.PluralMany},
		{-12, i18n.PluralMany},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.SlavicPluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestRomancePluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralOne},
		{1, i18n.PluralOne},
		{2, i18n.PluralOther},
		{10, i18n.PluralOther},
		{100, i18n.PluralOther},
		{999999, i18n.PluralOther},
		{1000000, i18n.PluralMany},
		{2000000, i18n.PluralMany},
		{-1, i18n.PluralOne},
		{-2, i18n.PluralOther},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.RomancePluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGermanicPluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralOther},
		{1, i18n.PluralOne},
		{2, i18n.PluralOther},
		{10, i18n.PluralOther},
		{100, i18n.PluralOther},
		{-1, i18n.PluralOne},
		{-2, i18n.PluralOther},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.GermanicPluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAsianPluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralOther},
		{1, i18n.PluralOther},
		{2, i18n.PluralOther},
		{10, i18n.PluralOther},
		{100, i18n.PluralOther},
		{1000000, i18n.PluralOther},
		{-1, i18n.PluralOther},
		{-100, i18n.PluralOther},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.AsianPluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestArabicPluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralZero},
		{1, i18n.PluralOne},
		{2, i18n.PluralTwo},
		{3, i18n.PluralFew},
		{4, i18n.PluralFew},
		{10, i18n.PluralFew},
		{103, i18n.PluralFew},
		{110, i18n.PluralFew},
		{203, i18n.PluralFew},
		{210, i18n.PluralFew},
		{11, i18n.PluralMany},
		{15, i18n.PluralMany},
		{20, i18n.PluralMany},
		{50, i18n.PluralMany},
		{99, i18n.PluralMany},
		{111, i18n.PluralMany},
		{150, i18n.PluralMany},
		{199, i18n.PluralMany},
		{100, i18n.PluralOther},
		{101, i18n.PluralOther},
		{102, i18n.PluralOther},
		{200, i18n.PluralOther},
		{300, i18n.PluralOther},
		{1000, i18n.PluralOther},
		{-1, i18n.PluralOne},
		{-2, i18n.PluralTwo},
		{-3, i18n.PluralFew},
		{-11, i18n.PluralMany},
		{-100, i18n.PluralOther},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.ArabicPluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSpanishPluralRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		expected string
	}{
		{0, i18n.PluralOther},
		{1, i18n.PluralOne},
		{2, i18n.PluralOther},
		{10, i18n.PluralOther},
		{100, i18n.PluralOther},
		{999999, i18n.PluralOther},
		{1000000, i18n.PluralMany},
		{2000000, i18n.PluralMany},
		{-1, i18n.PluralOne},
		{-2, i18n.PluralOther},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			result := i18n.SpanishPluralRule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPluralRuleForLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lang     string
		n        int
		expected string
	}{
		{"en", 1, i18n.PluralOne},
		{"en-US", 2, i18n.PluralOther},
		{"EN", 0, i18n.PluralZero},
		{"pl", 2, i18n.PluralFew},
		{"ru", 5, i18n.PluralMany},
		{"cs", 1, i18n.PluralOne},
		{"uk", 12, i18n.PluralMany},
		{"fr", 0, i18n.PluralOne},
		{"it", 1000000, i18n.PluralMany},
		{"pt-BR", 2, i18n.PluralOther},
		{"es", 1, i18n.PluralOne},
		{"es-MX", 1000000, i18n.PluralMany},
		{"de", 1, i18n.PluralOne},
		{"nl", 0, i18n.PluralOther},
		{"sv", 2, i18n.PluralOther},
		{"ja", 0, i18n.PluralOther},
		{"zh", 1, i18n.PluralOther},
		{"ko", 100, i18n.PluralOther},
		{"ar", 0, i18n.PluralZero},
		{"ar", 2, i18n.PluralTwo},
		{"ar", 3, i18n.PluralFew},
		{"ar", 11, i18n.PluralMany},
		{"xyz", 0, i18n.PluralZero},
		{"", 1, i18n.PluralOne},
		{"unknown", 2, i18n.PluralFew},
		{"e", 1, i18n.PluralOne},
		{"english", 1, i18n.PluralOne},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("lang=%s_n=%d", tt.lang, tt.n), func(t *testing.T) {
			t.Parallel()
			rule := i18n.GetPluralRuleForLanguage(tt.lang)
			result := rule(tt.n)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSupportedPluralForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rule     i18n.PluralRule
		expected []string
	}{
		{"Default", i18n.DefaultPluralRule, []string{i18n.PluralZero, i18n.PluralOne, i18n.PluralFew, i18n.PluralMany, i18n.PluralOther}},
		{"English", i18n.EnglishPluralRule, []string{i18n.PluralZero, i18n.PluralOne, i18n.PluralOther}},
		{"Slavic", i18n.SlavicPluralRule, []string{i18n.PluralZero, i18n.PluralOne, i18n.PluralFew, i18n.PluralMany}},
		{"Romance", i18n.RomancePluralRule, []string{i18n.PluralOne, i18n.PluralMany, i18n.PluralOther}},
		{"Germanic", i18n.GermanicPluralRule, []string{i18n.PluralOne, i18n.PluralOther}},
		{"Asian", i18n.AsianPluralRule, []string{i18n.PluralOther}},
		{"Arabic", i18n.ArabicPluralRule, []string{i18n.PluralZero, i18n.PluralOne, i18n.PluralTwo, i18n.PluralFew, i18n.PluralMany, i18n.PluralOther}},
		{"Spanish", i18n.SpanishPluralRule, []string{i18n.PluralOne, i18n.PluralMany, i18n.PluralOther}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := i18n.SupportedPluralForms(tt.rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkPluralRules(b *testing.B) {
	benchmarks := []struct {
		name string
		rule i18n.PluralRule
	}{
		{"Default", i18n.DefaultPluralRule},
		{"English", i18n.EnglishPluralRule},
		{"Slavic", i18n.SlavicPluralRule},
		{"Romance", i18n.RomancePluralRule},
		{"Germanic", i18n.GermanicPluralRule},
		{"Asian", i18n.AsianPluralRule},
		{"Arabic", i18n.ArabicPluralRule},
		{"Spanish", i18n.SpanishPluralRule},
	}

	testNumbers := []int{0, 1, 2, 3, 5, 11, 21, 100, 1000}

	for _, bench := range benchmarks {
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				for _, n := range testNumbers {
					_ = bench.rule(n)
				}
			}
		})
	}
}

func BenchmarkGetPluralRuleForLanguage(b *testing.B) {
	languages := []string{"en", "pl", "fr", "de", "ja", "ar", "es", "xyz"}

	b.ResetTimer()
	for b.Loop() {
		for _, lang := range languages {
			_ = i18n.GetPluralRuleForLanguage(lang)
		}
	}
}
