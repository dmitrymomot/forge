package slug_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/slug"
)

func TestMake(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		opts     []slug.Option
		expected string
	}{
		{
			name:     "simple text",
			input:    "Hello World",
			expected: "hello-world",
		},
		{
			name:     "with punctuation",
			input:    "Hello, World!",
			expected: "hello-world",
		},
		{
			name:     "with numbers",
			input:    "Product 123",
			expected: "product-123",
		},
		{
			name:     "multiple spaces",
			input:    "Too    Many     Spaces",
			expected: "too-many-spaces",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  Trim Me  ",
			expected: "trim-me",
		},
		{
			name:     "special characters",
			input:    "Price: $99.99",
			expected: "price-99-99",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "!@#$%^&*()",
			expected: "",
		},
		{
			name:     "unicode diacritics",
			input:    "Café résumé naïve",
			expected: "cafe-resume-naive",
		},
		{
			name:     "mixed case with lowercase false",
			input:    "Hello World",
			opts:     []slug.Option{slug.WithLowercase(false)},
			expected: "Hello-World",
		},
		{
			name:     "custom separator",
			input:    "Hello World",
			opts:     []slug.Option{slug.WithSeparator("_")},
			expected: "hello_world",
		},
		{
			name:     "max length",
			input:    "This is a very long title that should be truncated",
			opts:     []slug.Option{slug.WithMaxLength(20)},
			expected: "this-is-a-very-long",
		},
		{
			name:     "max length with separator",
			input:    "Cut off cleanly",
			opts:     []slug.Option{slug.WithMaxLength(7)},
			expected: "cut-off",
		},
		{
			name:     "strip specific characters",
			input:    "Remove (these) [chars]",
			opts:     []slug.Option{slug.WithStripChars("()[]")},
			expected: "remove-these-chars",
		},
		{
			name:  "custom replacements",
			input: "Fish & Chips @ Home",
			opts: []slug.Option{
				slug.WithCustomReplace(map[string]string{
					"&": "and",
					"@": "at",
				}),
			},
			expected: "fish-and-chips-at-home",
		},
		{
			name:     "consecutive separators",
			input:    "Too---Many---Dashes",
			expected: "too-many-dashes",
		},
		{
			name:     "german characters",
			input:    "Über Größe straße",
			expected: "uber-grose-strase",
		},
		{
			name:     "french characters",
			input:    "Château façade élève",
			expected: "chateau-facade-eleve",
		},
		{
			name:     "spanish characters",
			input:    "Niño español año",
			expected: "nino-espanol-ano",
		},
		{
			name:     "polish characters",
			input:    "Zażółć gęślą jaźń",
			expected: "zazolc-gesla-jazn",
		},
		{
			name:     "mixed unicode and ascii",
			input:    "Côte d'Ivoire 2024",
			expected: "cote-d-ivoire-2024",
		},
		{
			name:  "all options combined",
			input: "COMPLEX & Test @ 2024!!!",
			opts: []slug.Option{
				slug.WithSeparator("_"),
				slug.WithLowercase(false),
				slug.WithMaxLength(15),
				slug.WithStripChars("!"),
				slug.WithCustomReplace(map[string]string{
					"&": "AND",
					"@": "AT",
				}),
			},
			expected: "COMPLEX_AND_Tes",
		},
		{
			name:     "trailing separator should be removed",
			input:    "Ends with dash-",
			expected: "ends-with-dash",
		},
		{
			name:     "multiple trailing separators",
			input:    "Multiple---",
			expected: "multiple",
		},
		{
			name:     "only numbers",
			input:    "123456789",
			expected: "123456789",
		},
		{
			name:     "mixed numbers and letters",
			input:    "abc123def456",
			expected: "abc123def456",
		},
		{
			name:     "url with protocol",
			input:    "https://example.com",
			expected: "https-example-com",
		},
		{
			name:     "email address",
			input:    "user@example.com",
			expected: "user-example-com",
		},
		{
			name:     "path like string",
			input:    "path/to/file.txt",
			expected: "path-to-file-txt",
		},
		{
			name:     "emoji should be stripped",
			input:    "Hello 😀 World 🌍",
			expected: "hello-world",
		},
		{
			name:     "tabs and newlines",
			input:    "Line1\nLine2\tTabbed",
			expected: "line1-line2-tabbed",
		},
		{
			name:     "zero max length",
			input:    "Should not truncate",
			opts:     []slug.Option{slug.WithMaxLength(0)},
			expected: "should-not-truncate",
		},
		{
			name:     "empty separator",
			input:    "No Separator",
			opts:     []slug.Option{slug.WithSeparator("")},
			expected: "noseparator",
		},
		{
			name:     "multi-character separator",
			input:    "Multi Sep Test",
			opts:     []slug.Option{slug.WithSeparator("---")},
			expected: "multi---sep---test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.input, tt.opts...)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeDiacritic(t *testing.T) {
	t.Parallel()

	// Test specific diacritic conversions
	inputs := []struct {
		char     string
		expected string
	}{
		{"à", "a"}, {"á", "a"}, {"â", "a"}, {"ã", "a"}, {"ä", "a"}, {"å", "a"},
		{"À", "a"}, {"Á", "a"}, {"Â", "a"}, {"Ã", "a"}, {"Ä", "a"}, {"Å", "a"},
		{"è", "e"}, {"é", "e"}, {"ê", "e"}, {"ë", "e"},
		{"È", "e"}, {"É", "e"}, {"Ê", "e"}, {"Ë", "e"},
		{"ì", "i"}, {"í", "i"}, {"î", "i"}, {"ï", "i"},
		{"Ì", "i"}, {"Í", "i"}, {"Î", "i"}, {"Ï", "i"},
		{"ò", "o"}, {"ó", "o"}, {"ô", "o"}, {"õ", "o"}, {"ö", "o"}, {"ø", "o"},
		{"Ò", "o"}, {"Ó", "o"}, {"Ô", "o"}, {"Õ", "o"}, {"Ö", "o"}, {"Ø", "o"},
		{"ù", "u"}, {"ú", "u"}, {"û", "u"}, {"ü", "u"},
		{"Ù", "u"}, {"Ú", "u"}, {"Û", "u"}, {"Ü", "u"},
		{"ñ", "n"}, {"Ñ", "n"},
		{"ç", "c"}, {"Ç", "c"},
		{"ß", "s"},
		{"æ", "a"}, {"Æ", "a"},
		{"œ", "o"}, {"Œ", "o"},
	}

	for _, tt := range inputs {
		t.Run(tt.char, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.char)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestReservedSlugs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		opts     []slug.Option
		validate func(t *testing.T, result string)
	}{
		{
			name:  "basic reserved slug",
			input: "admin",
			opts:  []slug.Option{slug.WithReservedSlugs("admin", "api", "login")},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "admin", result)
				require.True(t, strings.HasPrefix(result, "admin-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2)
				require.Len(t, parts[1], 6) // default suffix length
			},
		},
		{
			name:  "case-insensitive reserved slug (uppercase input)",
			input: "ADMIN",
			opts:  []slug.Option{slug.WithReservedSlugs("admin")},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "admin", result)
				require.True(t, strings.HasPrefix(result, "admin-"))
			},
		},
		{
			name:  "case-insensitive reserved slug (mixed case input)",
			input: "AdMiN",
			opts:  []slug.Option{slug.WithReservedSlugs("admin")},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "admin", result)
				require.True(t, strings.HasPrefix(result, "admin-"))
			},
		},
		{
			name:  "reserved slug with uppercase output",
			input: "ADMIN",
			opts:  []slug.Option{slug.WithReservedSlugs("admin"), slug.WithLowercase(false)},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "ADMIN", result)
				require.True(t, strings.HasPrefix(result, "ADMIN-"))
				parts := strings.Split(result, "-")
				require.Regexp(t, "^[a-zA-Z0-9]+$", parts[1]) // mixed case suffix when Lowercase(false)
			},
		},
		{
			name:  "non-reserved slug passes through",
			input: "product",
			opts:  []slug.Option{slug.WithReservedSlugs("admin", "api", "login")},
			validate: func(t *testing.T, result string) {
				require.Equal(t, "product", result)
			},
		},
		{
			name:  "reserved slug with custom separator",
			input: "api",
			opts:  []slug.Option{slug.WithReservedSlugs("api"), slug.WithSeparator("_")},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "api", result)
				require.True(t, strings.HasPrefix(result, "api_"))
				parts := strings.Split(result, "_")
				require.Len(t, parts, 2)
			},
		},
		{
			name:  "reserved slug with explicit suffix",
			input: "login",
			opts:  []slug.Option{slug.WithReservedSlugs("login"), slug.WithSuffix(8)},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "login", result)
				require.True(t, strings.HasPrefix(result, "login-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2)
				require.Len(t, parts[1], 8) // explicit suffix length
			},
		},
		{
			name:  "reserved slug with max length",
			input: "admin",
			opts:  []slug.Option{slug.WithReservedSlugs("admin"), slug.WithMaxLength(10)},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "admin", result)
				require.LessOrEqual(t, len(result), 10)
				// With maxLength=10, "admin-" takes 6 chars, leaving 4 for suffix
				parts := strings.Split(result, "-")
				if len(parts) == 2 {
					require.Len(t, parts[1], 4)
				}
			},
		},
		{
			name:  "reserved slug too long for max length",
			input: "administrator",
			opts:  []slug.Option{slug.WithReservedSlugs("administrator"), slug.WithMaxLength(10)},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "administrator", result)
				require.LessOrEqual(t, len(result), 10)
			},
		},
		{
			name:  "multiple reserved slugs",
			input: "api endpoint",
			opts:  []slug.Option{slug.WithReservedSlugs("api-endpoint", "api", "endpoint")},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "api-endpoint", result)
				require.True(t, strings.HasPrefix(result, "api-endpoint-"))
			},
		},
		{
			name:  "reserved slug from slice expansion",
			input: "config",
			opts:  []slug.Option{slug.WithReservedSlugs([]string{"config", "system", "root"}...)},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "config", result)
				require.True(t, strings.HasPrefix(result, "config-"))
			},
		},
		{
			name:  "empty reserved slug list",
			input: "admin",
			opts:  []slug.Option{slug.WithReservedSlugs()},
			validate: func(t *testing.T, result string) {
				require.Equal(t, "admin", result)
			},
		},
		{
			name:  "reserved slug case variations in list",
			input: "admin",
			opts:  []slug.Option{slug.WithReservedSlugs("ADMIN", "Admin", "admin")},
			validate: func(t *testing.T, result string) {
				require.NotEqual(t, "admin", result)
				require.True(t, strings.HasPrefix(result, "admin-"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.input, tt.opts...)
			tt.validate(t, result)
		})
	}
}

func BenchmarkMake(b *testing.B) {
	testCases := []struct {
		name  string
		input string
		opts  []slug.Option
	}{
		{
			name:  "simple",
			input: "Hello World",
		},
		{
			name:  "with_diacritics",
			input: "Café résumé naïve",
		},
		{
			name:  "long_text",
			input: "This is a very long title that contains many words and should test the performance of the slug generation",
		},
		{
			name:  "with_options",
			input: "Complex & Test @ 2024",
			opts: []slug.Option{
				slug.WithMaxLength(20),
				slug.WithCustomReplace(map[string]string{"&": "and", "@": "at"}),
			},
		},
		{
			name:  "unicode_heavy",
			input: "Ñoño español año château façade über größe",
		},
		{
			name:  "special_chars_heavy",
			input: "!@#$%^&*()_+{}|:\"<>?[]\\;',./",
		},
		{
			name:  "with_suffix",
			input: "Product Name",
			opts:  []slug.Option{slug.WithSuffix(6)},
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = slug.Make(tc.input, tc.opts...)
			}
		})
	}
}

func BenchmarkMakeParallel(b *testing.B) {
	input := "This is a sample text with some special characters: !@#$%"
	opts := []slug.Option{slug.WithMaxLength(50)}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = slug.Make(input, opts...)
		}
	})
}

func TestGenerateSuffixHappyPath(t *testing.T) {
	t.Parallel()

	// Verifies that the random-suffix generator produces valid, correctly-sized,
	// charset-respecting output across a range of lengths. The deterministic
	// fallback path (rand.Read failure) is covered separately in the internal
	// test TestGenerateSuffixFallback, which forces randRead to fail.
	tests := []struct {
		name      string
		length    int
		lowercase bool
	}{
		{
			name:      "zero length",
			length:    0,
			lowercase: true,
		},
		{
			name:      "small length lowercase",
			length:    1,
			lowercase: true,
		},
		{
			name:      "small length uppercase allowed",
			length:    1,
			lowercase: false,
		},
		{
			name:      "large length",
			length:    100,
			lowercase: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for range 10 {
				opts := []slug.Option{slug.WithSuffix(tt.length)}
				if !tt.lowercase {
					opts = append(opts, slug.WithLowercase(false))
				}
				result := slug.Make("test", opts...)
				if tt.length > 0 {
					parts := strings.Split(result, "-")
					suffix := parts[len(parts)-1]
					require.Len(t, suffix, tt.length)
					if tt.lowercase {
						require.Regexp(t, "^[a-z0-9]+$", suffix)
					} else {
						require.Regexp(t, "^[a-zA-Z0-9]+$", suffix)
					}
				}
			}
		})
	}
}

func TestMaxLengthEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		opts     []slug.Option
		validate func(t *testing.T, result string)
	}{
		{
			name:  "max length smaller than suffix",
			input: "Test",
			opts:  []slug.Option{slug.WithSuffix(10), slug.WithMaxLength(5)},
			validate: func(t *testing.T, result string) {
				require.LessOrEqual(t, len(result), 5)
				// Should be just truncated suffix
				require.Regexp(t, "^[a-z0-9]{5}$", result)
			},
		},
		{
			name:  "max length exactly suffix length",
			input: "Test",
			opts:  []slug.Option{slug.WithSuffix(8), slug.WithMaxLength(8)},
			validate: func(t *testing.T, result string) {
				require.Equal(t, 8, len(result))
				require.Regexp(t, "^[a-z0-9]{8}$", result)
			},
		},
		{
			name:  "max length with multi-byte separator",
			input: "Test Case",
			opts:  []slug.Option{slug.WithSuffix(4), slug.WithMaxLength(15), slug.WithSeparator("---")},
			validate: func(t *testing.T, result string) {
				require.LessOrEqual(t, len(result), 15)
				require.Contains(t, result, "---")
			},
		},
		{
			name:  "very small max length with suffix",
			input: "Long Title Here",
			opts:  []slug.Option{slug.WithSuffix(3), slug.WithMaxLength(5)},
			validate: func(t *testing.T, result string) {
				require.LessOrEqual(t, len(result), 5)
				// Should be "l-abc" or similar
				parts := strings.Split(result, "-")
				if len(parts) > 1 {
					require.Len(t, parts[len(parts)-1], 3)
				}
			},
		},
		{
			name:  "max length cuts in middle of rune",
			input: "Test™Case", // ™ is multi-byte
			opts:  []slug.Option{slug.WithMaxLength(6)},
			validate: func(t *testing.T, result string) {
				// "Test™Case" becomes "test-case" but truncated to 6 chars = "test-c"
				require.Equal(t, "test-c", result)
			},
		},
		{
			name:  "empty input with suffix and max length",
			input: "",
			opts:  []slug.Option{slug.WithSuffix(10), slug.WithMaxLength(5)},
			validate: func(t *testing.T, result string) {
				require.Equal(t, 5, len(result))
				require.Regexp(t, "^[a-z0-9]{5}$", result)
			},
		},
		{
			name:  "suffix with no room after max length truncation",
			input: "VeryLongTitleThatNeedsToBeShortened",
			opts:  []slug.Option{slug.WithSuffix(6), slug.WithMaxLength(8)},
			validate: func(t *testing.T, result string) {
				// Should be "v-abc123" (1 char + separator + 6 char suffix = 8)
				require.Equal(t, 8, len(result))
				parts := strings.Split(result, "-")
				require.Len(t, parts[len(parts)-1], 6)
			},
		},
		{
			// Explicit suffix is a fixed-width token: when the multi-byte
			// separator plus suffix consume the entire budget, the slug is
			// dropped and only the suffix (capped at maxLength) is emitted.
			name:  "explicit suffix no room with multi-byte separator drops slug",
			input: "Something Long",
			opts:  []slug.Option{slug.WithSuffix(8), slug.WithMaxLength(8), slug.WithSeparator("---")},
			validate: func(t *testing.T, result string) {
				require.Equal(t, 8, len(result))
				// Slug dropped, no separator, full 8-char suffix.
				require.Regexp(t, "^[a-z0-9]{8}$", result)
				require.NotContains(t, result, "---")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.input, tt.opts...)
			tt.validate(t, result)
		})
	}
}

func TestMinLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		opts      []slug.Option
		checkFunc func(t *testing.T, result string)
	}{
		{
			name:  "short input gets padded with 6-char suffix",
			input: "owl",
			opts:  []slug.Option{slug.WithMinLength(10)},
			checkFunc: func(t *testing.T, result string) {
				// "owl" (3 chars) + "-" (1 char) + 6-char suffix = 10 total
				require.Equal(t, 10, len(result))
				require.True(t, strings.HasPrefix(result, "owl-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2)
				require.Equal(t, "owl", parts[0])
				require.Len(t, parts[1], 6) // Fixed 6-character suffix
				require.Regexp(t, "^[a-z0-9]{6}$", parts[1])
			},
		},
		{
			name:  "input already meeting minimum length unchanged",
			input: "hello",
			opts:  []slug.Option{slug.WithMinLength(5)},
			checkFunc: func(t *testing.T, result string) {
				require.Equal(t, "hello", result)
			},
		},
		{
			name:  "input exceeding minimum length unchanged",
			input: "hello world",
			opts:  []slug.Option{slug.WithMinLength(5)},
			checkFunc: func(t *testing.T, result string) {
				require.Equal(t, "hello-world", result)
			},
		},
		{
			name:  "zero minimum length has no effect",
			input: "hi",
			opts:  []slug.Option{slug.WithMinLength(0)},
			checkFunc: func(t *testing.T, result string) {
				require.Equal(t, "hi", result)
			},
		},
		{
			name:  "min length with custom separator",
			input: "cat",
			opts:  []slug.Option{slug.WithMinLength(8), slug.WithSeparator("_")},
			checkFunc: func(t *testing.T, result string) {
				// "cat" (3 chars) + "_" (1 char) + 6-char suffix = 10 total
				require.Equal(t, 10, len(result))
				require.True(t, strings.HasPrefix(result, "cat_"))
				parts := strings.Split(result, "_")
				require.Equal(t, "cat", parts[0])
				require.Len(t, parts[1], 6) // Fixed 6-character suffix
				require.Regexp(t, "^[a-z0-9]{6}$", parts[1])
			},
		},
		{
			name:  "min length with uppercase disabled",
			input: "Cat",
			opts:  []slug.Option{slug.WithMinLength(10), slug.WithLowercase(false)},
			checkFunc: func(t *testing.T, result string) {
				// "Cat" (3 chars) + "-" (1 char) + 6-char suffix = 10 total
				require.Equal(t, 10, len(result))
				require.True(t, strings.HasPrefix(result, "Cat-"))
				parts := strings.Split(result, "-")
				require.Equal(t, "Cat", parts[0])
				require.Len(t, parts[1], 6) // Fixed 6-character suffix
				require.Regexp(t, "^[a-zA-Z0-9]{6}$", parts[1])
			},
		},
		{
			name:  "min length exactly equals base slug length",
			input: "test",
			opts:  []slug.Option{slug.WithMinLength(4)},
			checkFunc: func(t *testing.T, result string) {
				require.Equal(t, "test", result)
			},
		},
		{
			name:  "min length one more than base slug gets 6-char suffix",
			input: "test",
			opts:  []slug.Option{slug.WithMinLength(5)},
			checkFunc: func(t *testing.T, result string) {
				// "test" is 4 chars, MinLength is 5
				// With new behavior: always adds 6-char suffix when below minLength
				// "test" (4 chars) + "-" (1 char) + 6-char suffix = 11 total
				require.Equal(t, 11, len(result))
				require.True(t, strings.HasPrefix(result, "test-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts[1], 6) // Fixed 6-character suffix
			},
		},
		{
			name:  "min length two more than base slug gets 6-char suffix",
			input: "test",
			opts:  []slug.Option{slug.WithMinLength(6)},
			checkFunc: func(t *testing.T, result string) {
				// "test" is 4 chars, MinLength is 6
				// With new behavior: always adds 6-char suffix when below minLength
				// "test" (4 chars) + "-" (1 char) + 6-char suffix = 11 total
				require.Equal(t, 11, len(result))
				require.True(t, strings.HasPrefix(result, "test-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts[1], 6) // Fixed 6-character suffix
			},
		},
		{
			name:  "min length with empty separator",
			input: "go",
			opts:  []slug.Option{slug.WithMinLength(6), slug.WithSeparator("")},
			checkFunc: func(t *testing.T, result string) {
				// "go" (2 chars) + "" (0 chars) + 6-char suffix = 8 total
				require.Equal(t, 8, len(result))
				require.True(t, strings.HasPrefix(result, "go"))
				// No separator, so suffix directly appended
				require.Regexp(t, "^go[a-z0-9]{6}$", result)
			},
		},
		{
			name:  "min length with multi-character separator",
			input: "xy",
			opts:  []slug.Option{slug.WithMinLength(10), slug.WithSeparator("---")},
			checkFunc: func(t *testing.T, result string) {
				// "xy" (2 chars) + "---" (3 chars) + 6-char suffix = 11 total
				require.Equal(t, 11, len(result))
				require.True(t, strings.HasPrefix(result, "xy---"))
				parts := strings.Split(result, "---")
				require.Equal(t, "xy", parts[0])
				require.Len(t, parts[1], 6) // Fixed 6-character suffix
				require.Regexp(t, "^[a-z0-9]{6}$", parts[1])
			},
		},
		{
			name:  "empty input with min length",
			input: "",
			opts:  []slug.Option{slug.WithMinLength(8)},
			checkFunc: func(t *testing.T, result string) {
				// Empty input gets a 6-char random suffix (no separator since result is empty)
				// With new behavior: always uses 6-char suffix
				require.Equal(t, 6, len(result))
				require.Regexp(t, "^[a-z0-9]{6}$", result)
			},
		},
		{
			name:  "min length padding is random each time",
			input: "dog",
			opts:  []slug.Option{slug.WithMinLength(10)},
			checkFunc: func(t *testing.T, result string) {
				result2 := slug.Make("dog", slug.WithMinLength(10))
				// "dog" (3 chars) + "-" (1 char) + 6-char suffix = 10 total
				require.Equal(t, 10, len(result))
				require.Equal(t, 10, len(result2))
				// Both start with "dog-"
				require.True(t, strings.HasPrefix(result, "dog-"))
				require.True(t, strings.HasPrefix(result2, "dog-"))
				// But suffixes should differ (randomized)
				parts1 := strings.Split(result, "-")
				parts2 := strings.Split(result2, "-")
				require.Len(t, parts1[1], 6)
				require.Len(t, parts2[1], 6)
				require.NotEqual(t, parts1[1], parts2[1])
			},
		},
		{
			name:  "min length with diacritics",
			input: "café",
			opts:  []slug.Option{slug.WithMinLength(10)},
			checkFunc: func(t *testing.T, result string) {
				// "café" becomes "cafe" (4 chars), needs padding with 6-char suffix
				// "cafe" (4 chars) + "-" (1 char) + 6-char suffix = 11 total
				require.Equal(t, 11, len(result))
				require.True(t, strings.HasPrefix(result, "cafe-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts[1], 6)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.input, tt.opts...)
			tt.checkFunc(t, result)
		})
	}
}

func TestMinLengthWithMaxLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		opts      []slug.Option
		checkFunc func(t *testing.T, result string)
	}{
		{
			name:  "min length smaller than max length",
			input: "cat",
			opts:  []slug.Option{slug.WithMinLength(10), slug.WithMaxLength(15)},
			checkFunc: func(t *testing.T, result string) {
				// "cat" (3 chars) + "-" (1 char) + 6-char suffix = 10 total
				// This fits within maxLength of 15
				require.Equal(t, 10, len(result))
				require.True(t, strings.HasPrefix(result, "cat-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts[1], 6)
			},
		},
		{
			name:  "min length equals max length",
			input: "dog",
			opts:  []slug.Option{slug.WithMinLength(10), slug.WithMaxLength(10)},
			checkFunc: func(t *testing.T, result string) {
				// "dog" (3 chars) + "-" (1 char) + 6-char suffix = 10 total
				// Perfectly fits min and max
				require.Equal(t, 10, len(result))
				require.True(t, strings.HasPrefix(result, "dog-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts[1], 6)
			},
		},
		{
			name:  "min length larger than max length - max takes priority",
			input: "bird",
			opts:  []slug.Option{slug.WithMinLength(20), slug.WithMaxLength(10)},
			checkFunc: func(t *testing.T, result string) {
				// MinLength wants 6-char suffix, but maxLength constrains it
				// "bird" (4 chars) + "-" (1 char) + suffix fits in maxLength 10
				// Available space: 10 - 4 - 1 = 5 chars for suffix
				require.Equal(t, 10, len(result))
				require.True(t, strings.HasPrefix(result, "bird-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts[1], 5) // Truncated to fit maxLength
			},
		},
		{
			name:  "long input with min and max length",
			input: "this is a very long title that needs truncation",
			opts:  []slug.Option{slug.WithMinLength(10), slug.WithMaxLength(20)},
			checkFunc: func(t *testing.T, result string) {
				// Input is already long, so maxLength applies
				require.LessOrEqual(t, len(result), 20)
			},
		},
		{
			name:  "short input meeting max but not min",
			input: "xyz",
			opts:  []slug.Option{slug.WithMinLength(10), slug.WithMaxLength(50)},
			checkFunc: func(t *testing.T, result string) {
				// MinLength applies with 6-char suffix
				// "xyz" (3 chars) + "-" (1 char) + 6-char suffix = 10 total
				require.Equal(t, 10, len(result))
				require.True(t, strings.HasPrefix(result, "xyz-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts[1], 6)
			},
		},
		{
			name:  "min and max with custom separator",
			input: "ab",
			opts:  []slug.Option{slug.WithMinLength(8), slug.WithMaxLength(12), slug.WithSeparator("_")},
			checkFunc: func(t *testing.T, result string) {
				// "ab" (2 chars) + "_" (1 char) + 6-char suffix = 9 total
				// Fits within maxLength of 12
				require.Equal(t, 9, len(result))
				require.True(t, strings.HasPrefix(result, "ab_"))
				parts := strings.Split(result, "_")
				require.Len(t, parts[1], 6)
			},
		},
		{
			// Regression for the min+max "no room for full suffix" path: the
			// slug already nearly fills maxLength while being below minLength.
			// The real slug content must be preserved and the padding suffix
			// shrunk to fit; maxLength is the hard cap, so minLength (which
			// exceeds maxLength here) cannot be fully honored.
			name:  "min length below max but suffix does not fully fit keeps slug",
			input: "report",
			opts:  []slug.Option{slug.WithMinLength(10), slug.WithMaxLength(8)},
			checkFunc: func(t *testing.T, result string) {
				// "report" (6) + "-" (1) leaves only 1 char for the suffix.
				require.Equal(t, 8, len(result))
				require.LessOrEqual(t, len(result), 8) // maxLength respected
				require.True(t, strings.HasPrefix(result, "report-"))
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2)
				require.Equal(t, "report", parts[0]) // real content NOT discarded
				require.Len(t, parts[1], 1)
				require.Regexp(t, "^[a-z0-9]$", parts[1])
			},
		},
		{
			// Regression for the original bug at slug.go:256-263 where the
			// "no room" path discarded the slug and produced a bare suffix that
			// UNDERSHOT minLength (e.g. a single character). With a multi-byte
			// separator, slug + separator alone consumes the whole budget. The
			// result must still respect maxLength and not undershoot to a tiny
			// fragment: it fills maxLength (== minLength) with a suffix.
			name:  "min length no room at all with multi-byte separator",
			input: "ab",
			opts:  []slug.Option{slug.WithMinLength(4), slug.WithMaxLength(4), slug.WithSeparator("---")},
			checkFunc: func(t *testing.T, result string) {
				// maxLength(4) == minLength(4); "ab" + "---" already fills the
				// budget, so the suffix-only result fills exactly maxLength.
				require.Equal(t, 4, len(result))           // respects maxLength
				require.GreaterOrEqual(t, len(result), 4)  // does NOT undershoot minLength
				require.Regexp(t, "^[a-z0-9]{4}$", result) // valid charset, no stray separator
				require.NotContains(t, result, "---")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.input, tt.opts...)
			tt.checkFunc(t, result)
		})
	}
}

func TestMinLengthWithOtherOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		opts      []slug.Option
		checkFunc func(t *testing.T, result string)
	}{
		{
			name:  "min length with reserved slugs uses single cooperating suffix",
			input: "api",
			opts:  []slug.Option{slug.WithMinLength(15), slug.WithReservedSlugs("api")},
			checkFunc: func(t *testing.T, result string) {
				// A reserved-slug collision and the min-length requirement are both
				// satisfied by ONE 6-char suffix, not two stacked suffixes.
				// "api" (3) + "-" (1) + 6-char suffix = 10 total.
				require.NotEqual(t, "api", result)
				require.True(t, strings.HasPrefix(result, "api-"))
				require.Equal(t, 10, len(result))
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2) // api, single suffix (no double suffix)
				require.Equal(t, "api", parts[0])
				require.Len(t, parts[1], 6)
				require.Regexp(t, "^[a-z0-9]{6}$", parts[1])
			},
		},
		{
			name:  "min length with suffix option uses single suffix",
			input: "test",
			opts:  []slug.Option{slug.WithMinLength(15), slug.WithSuffix(8)},
			checkFunc: func(t *testing.T, result string) {
				// An explicit WithSuffix already satisfies the "needs a suffix"
				// requirement; the min-length check must not stack a second one.
				// "test" (4) + "-" (1) + 8-char suffix = 13 total.
				require.Equal(t, 13, len(result))
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2) // test, single 8-char suffix
				require.Equal(t, "test", parts[0])
				require.Len(t, parts[1], 8)
				require.Regexp(t, "^[a-z0-9]{8}$", parts[1])
			},
		},
		{
			name:  "min length with custom replace",
			input: "a&b",
			opts: []slug.Option{
				slug.WithMinLength(10),
				slug.WithCustomReplace(map[string]string{"&": "and"}),
			},
			checkFunc: func(t *testing.T, result string) {
				// "a&b" with CustomReplace becomes "aandb" (5 chars), below minLength of 10
				// Adds 6-char suffix: "aandb-xxxxxx" (12 chars)
				require.Equal(t, 12, len(result))
				require.True(t, strings.Contains(result, "aandb"))
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2)    // aandb, suffix
				require.Len(t, parts[1], 6) // 6-char suffix
			},
		},
		{
			name:  "min length with strip chars",
			input: "x[y]",
			opts:  []slug.Option{slug.WithMinLength(8), slug.WithStripChars("[]")},
			checkFunc: func(t *testing.T, result string) {
				// "x[y]" with StripChars becomes "xy" (2 chars), below minLength of 8
				// Adds 6-char suffix: "xy-xxxxxx" (9 chars)
				require.Equal(t, 9, len(result))
				require.NotContains(t, result, "[")
				require.NotContains(t, result, "]")
				parts := strings.Split(result, "-")
				require.Len(t, parts, 2)    // xy, suffix
				require.Len(t, parts[1], 6) // 6-char suffix
			},
		},
		{
			name:  "min length with all options",
			input: "a",
			opts: []slug.Option{
				slug.WithMinLength(12),
				slug.WithMaxLength(15),
				slug.WithSeparator("_"),
				slug.WithLowercase(false),
			},
			checkFunc: func(t *testing.T, result string) {
				// "a" (1 char) + "_" (1 char) + 6-char suffix = 8 total
				// This is less than minLength of 12, so would try to add suffix
				// But wait, 8 < 12, so it would add another suffix? No, the implementation
				// checks at line 196 AFTER the first suffix logic runs, but the first suffix
				// logic only runs if needsSuffix is true (reserved or WithSuffix), which isn't the case here.
				// So just one 6-char suffix is added: "a_xxxxxx" = 8 chars, which fits in maxLength of 15
				require.Equal(t, 8, len(result))
				require.True(t, strings.HasPrefix(result, "a_"))
				parts := strings.Split(result, "_")
				require.Len(t, parts[1], 6)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.input, tt.opts...)
			tt.checkFunc(t, result)
		})
	}
}

func TestMakeWithSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		opts      []slug.Option
		checkFunc func(t *testing.T, result string)
	}{
		{
			name:  "basic suffix",
			input: "Hello World",
			opts:  []slug.Option{slug.WithSuffix(6)},
			checkFunc: func(t *testing.T, result string) {
				parts := strings.Split(result, "-")
				require.Equal(t, "hello", parts[0])
				require.Equal(t, "world", parts[1])
				require.Len(t, parts[2], 6) // suffix should be 6 chars
				// Check suffix is alphanumeric lowercase
				require.Regexp(t, "^[a-z0-9]{6}$", parts[2])
			},
		},
		{
			name:  "suffix with uppercase disabled",
			input: "Test",
			opts:  []slug.Option{slug.WithSuffix(8), slug.WithLowercase(false)},
			checkFunc: func(t *testing.T, result string) {
				parts := strings.Split(result, "-")
				require.Equal(t, "Test", parts[0])
				require.Len(t, parts[1], 8)
				// Check suffix can contain uppercase
				require.Regexp(t, "^[a-zA-Z0-9]{8}$", parts[1])
			},
		},
		{
			name:  "suffix with custom separator",
			input: "Product",
			opts:  []slug.Option{slug.WithSuffix(4), slug.WithSeparator("_")},
			checkFunc: func(t *testing.T, result string) {
				parts := strings.Split(result, "_")
				require.Equal(t, "product", parts[0])
				require.Len(t, parts[1], 4)
			},
		},
		{
			name:  "suffix with max length",
			input: "Very Long Title Here",
			opts:  []slug.Option{slug.WithSuffix(6), slug.WithMaxLength(20)},
			checkFunc: func(t *testing.T, result string) {
				require.LessOrEqual(t, len(result), 20)
				require.Contains(t, result, "-") // Should have separator
				parts := strings.Split(result, "-")
				lastPart := parts[len(parts)-1]
				require.Len(t, lastPart, 6) // suffix should still be 6 chars
			},
		},
		{
			name:  "suffix longer than max length",
			input: "Test",
			opts:  []slug.Option{slug.WithSuffix(10), slug.WithMaxLength(8)},
			checkFunc: func(t *testing.T, result string) {
				// Should just be the suffix truncated
				require.Len(t, result, 8)
				require.Regexp(t, "^[a-z0-9]{8}$", result)
			},
		},
		{
			name:  "empty input with suffix",
			input: "",
			opts:  []slug.Option{slug.WithSuffix(5)},
			checkFunc: func(t *testing.T, result string) {
				require.Len(t, result, 5)
				require.Regexp(t, "^[a-z0-9]{5}$", result)
			},
		},
		{
			name:  "zero length suffix",
			input: "Normal Slug",
			opts:  []slug.Option{slug.WithSuffix(0)},
			checkFunc: func(t *testing.T, result string) {
				require.Equal(t, "normal-slug", result)
			},
		},
		{
			name:  "suffix preserves uniqueness",
			input: "Same Title",
			opts:  []slug.Option{slug.WithSuffix(6)},
			checkFunc: func(t *testing.T, result string) {
				// Generate another one and check they're different
				result2 := slug.Make("Same Title", slug.WithSuffix(6))
				require.NotEqual(t, result, result2)
				// But the base should be the same
				parts1 := strings.Split(result, "-")
				parts2 := strings.Split(result2, "-")
				require.Equal(t, parts1[0], parts2[0])
				require.Equal(t, parts1[1], parts2[1])
				require.NotEqual(t, parts1[2], parts2[2]) // suffixes should differ
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := slug.Make(tt.input, tt.opts...)
			tt.checkFunc(t, result)
		})
	}
}
