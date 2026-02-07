package i18n_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

func TestParseAcceptLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		available []string
		expected  string
	}{
		{
			name:      "empty header returns first available",
			header:    "",
			available: []string{"en", "pl", "de"},
			expected:  "en",
		},
		{
			name:      "empty available returns empty",
			header:    "en-US,en;q=0.9",
			available: []string{},
			expected:  "",
		},
		{
			name:      "exact match",
			header:    "pl",
			available: []string{"en", "pl", "de"},
			expected:  "pl",
		},
		{
			name:      "match with quality values",
			header:    "de;q=0.5,pl;q=0.9,en;q=0.8",
			available: []string{"en", "pl", "de"},
			expected:  "pl",
		},
		{
			name:      "language with region matches base",
			header:    "en-US",
			available: []string{"en", "pl", "de"},
			expected:  "en",
		},
		{
			name:      "base language matches regional variant",
			header:    "en",
			available: []string{"en-US", "pl", "de"},
			expected:  "en-US",
		},
		{
			name:      "multiple languages with decreasing quality",
			header:    "fr,en-US;q=0.9,en;q=0.8,pl;q=0.7",
			available: []string{"pl", "en"},
			expected:  "en",
		},
		{
			name:      "no match returns first available",
			header:    "fr,es,it",
			available: []string{"en", "pl", "de"},
			expected:  "en",
		},
		{
			name:      "complex header with multiple regions",
			header:    "en-GB,en-US;q=0.9,en;q=0.8,pl-PL;q=0.7,pl;q=0.6",
			available: []string{"pl", "en"},
			expected:  "en",
		},
		{
			name:      "case insensitive matching",
			header:    "EN-us,PL;q=0.9",
			available: []string{"pl", "en"},
			expected:  "en",
		},
		{
			name:      "whitespace handling",
			header:    " en , pl ; q=0.9 , de ; q=0.8 ",
			available: []string{"de", "pl"},
			expected:  "pl",
		},
		{
			name:      "invalid quality value defaults to 1.0",
			header:    "en;q=invalid,pl;q=0.5",
			available: []string{"en", "pl"},
			expected:  "en",
		},
		{
			name:      "wildcard is ignored",
			header:    "*,en;q=0.5",
			available: []string{"en", "pl"},
			expected:  "en",
		},
		{
			name:      "first match wins for same quality",
			header:    "en,pl",
			available: []string{"pl", "en"},
			expected:  "pl",
		},
		{
			name:      "regional variant exact match preferred",
			header:    "en-US,en;q=0.9",
			available: []string{"en", "en-US"},
			expected:  "en-US",
		},
		{
			name:      "oversized header is truncated safely",
			header:    strings.Repeat("en,", 2000) + "pl",
			available: []string{"en", "pl", "de"},
			expected:  "en",
		},
		{
			name:      "quality values outside 0-1 range default to 1.0",
			header:    "en;q=2.5,pl;q=-0.5,de;q=0.5",
			available: []string{"en", "pl", "de"},
			expected:  "en",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := i18n.ParseAcceptLanguage(tt.header, tt.available)
			require.Equal(t, tt.expected, result)
		})
	}
}
