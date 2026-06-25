package randomname_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/randomname"
)

func TestDefaultOptions(t *testing.T) {
	t.Parallel()
	opts := randomname.Generate(nil)
	require.NotEmpty(t, opts)
	// Should generate with default pattern
	require.Regexp(t, `^[a-z]+-[a-z]+$`, opts)
}

func TestOptionsMerge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		opts     *randomname.Options
		expected string
	}{
		{
			name:     "nil options uses defaults",
			opts:     nil,
			expected: `^[a-z]+-[a-z]+$`,
		},
		{
			name: "custom separator",
			opts: &randomname.Options{
				Separator: "_",
			},
			expected: `^[a-z]+_[a-z]+$`,
		},
		{
			name: "custom pattern",
			opts: &randomname.Options{
				Pattern: []randomname.WordType{randomname.Color, randomname.Noun},
			},
			expected: `^[a-z]+-[a-z]+$`,
		},
		{
			name: "empty pattern falls back to default",
			opts: &randomname.Options{
				Pattern: []randomname.WordType{},
			},
			expected: `^[a-z]+-[a-z]+$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name := randomname.Generate(tt.opts)
			require.Regexp(t, tt.expected, name)
		})
	}
}

func TestSuffixTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		suffix  randomname.SuffixType
		pattern string
	}{
		{
			name:    "no suffix",
			suffix:  randomname.NoSuffix,
			pattern: `^[a-z]+-[a-z]+$`,
		},
		{
			name:    "hex6 suffix",
			suffix:  randomname.Hex6,
			pattern: `^[a-z]+-[a-z]+-[0-9a-f]{6}$`,
		},
		{
			name:    "hex8 suffix",
			suffix:  randomname.Hex8,
			pattern: `^[a-z]+-[a-z]+-[0-9a-f]{8}$`,
		},
		{
			name:    "numeric4 suffix",
			suffix:  randomname.Numeric4,
			pattern: `^[a-z]+-[a-z]+-\d{4}$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name := randomname.Generate(&randomname.Options{
				Suffix: tt.suffix,
			})
			require.Regexp(t, tt.pattern, name)
		})
	}
}

func TestWordTypes(t *testing.T) {
	t.Parallel()
	// Test that all word types have words defined
	wordTypes := []randomname.WordType{
		randomname.Adjective,
		randomname.Noun,
		randomname.Color,
		randomname.Size,
		randomname.Origin,
		randomname.Action,
	}

	for _, wordType := range wordTypes {
		t.Run("word type availability", func(t *testing.T) {
			t.Parallel()
			// Generate with single word type
			name := randomname.Generate(&randomname.Options{
				Pattern: []randomname.WordType{wordType},
			})
			require.NotEmpty(t, name)
			require.Regexp(t, `^[a-z]+$`, name)
		})
	}
}

func TestValidator(t *testing.T) {
	t.Parallel()
	t.Run("accept first attempt", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		name := randomname.Generate(&randomname.Options{
			Validator: func(s string) bool {
				attempts++
				return true
			},
		})
		require.NotEmpty(t, name)
		require.Equal(t, 1, attempts)
	})

	t.Run("reject first few attempts", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		name := randomname.Generate(&randomname.Options{
			Validator: func(s string) bool {
				attempts++
				return attempts >= 3
			},
		})
		require.NotEmpty(t, name)
		require.Equal(t, 3, attempts)
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		name := randomname.Generate(&randomname.Options{
			Validator: func(s string) bool {
				attempts++
				return false // Always reject
			},
		})
		require.NotEmpty(t, name)
		require.Equal(t, 100, attempts) // Should try exactly 100 times
	})
}
