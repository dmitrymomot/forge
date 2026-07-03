package slug_test

import (
	"regexp"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/slug"
)

func TestMake_ReservedSlugs(t *testing.T) {
	// A reserved word gets a random 6-char suffix.
	reReserved := regexp.MustCompile(`^admin-[a-z0-9]{6}$`)
	got := slug.Make("Admin", slug.WithReservedSlugs("admin", "api"))
	assert.Regexp(t, reReserved, got, "reserved slug should be suffixed")

	// Case-insensitive matching of the reserved set.
	got2 := slug.Make("API", slug.WithReservedSlugs("admin", "api"))
	assert.Regexp(t, regexp.MustCompile(`^api-[a-z0-9]{6}$`), got2)

	// A non-reserved word is untouched.
	assert.Equal(t, "dashboard", slug.Make("Dashboard", slug.WithReservedSlugs("admin", "api")))
}

func TestMake_MinLength(t *testing.T) {
	// "ab" is shorter than 8 ⇒ pad with a random suffix; total >= 8 runes.
	got := slug.Make("ab", slug.WithMinLength(8))
	assert.GreaterOrEqual(t, utf8.RuneCountInString(got), 8, "Make(min=8) => %q", got)
	assert.Regexp(t, regexp.MustCompile(`^ab-[a-z0-9]+$`), got)

	// Already long enough ⇒ unchanged.
	assert.Equal(t, "hello-world", slug.Make("hello world", slug.WithMinLength(5)))
}

func TestMake_MinLength_EmptyBase(t *testing.T) {
	// No sluggable base + a minimum ⇒ a random-only slug of at least min runes.
	got := slug.Make("你好", slug.WithMinLength(6))
	assert.GreaterOrEqual(t, utf8.RuneCountInString(got), 6)
	assert.Regexp(t, regexp.MustCompile(`^[a-z0-9]+$`), got)
}

func TestMake_MaxLength_WithSuffix_NeverExceedsCap(t *testing.T) {
	// The total (base + separator + suffix) must never exceed maxLength.
	for range 50 {
		got := slug.Make("hello world foobar baz", slug.WithMaxLength(12), slug.WithSuffix(4))
		assert.LessOrEqual(t, utf8.RuneCountInString(got), 12, "got %q exceeds cap", got)
		assert.Regexp(t, regexp.MustCompile(`-[a-z0-9]{4}$`), got, "suffix preserved: %q", got)
	}
}

func TestMake_MaxLength_WithReserved_NeverExceedsCap(t *testing.T) {
	for range 50 {
		got := slug.Make("administrator", slug.WithMaxLength(10), slug.WithReservedSlugs("administrator"))
		assert.LessOrEqual(t, utf8.RuneCountInString(got), 10, "got %q exceeds cap", got)
	}
}
