package slug_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/slug"
)

func TestMake_WithSuffix(t *testing.T) {
	// "hello-world" + "-" + 6-char [a-z0-9] suffix.
	re := regexp.MustCompile(`^hello-world-[a-z0-9]{6}$`)
	for range 50 {
		got := slug.Make("Hello World", slug.WithSuffix(6))
		assert.Regexp(t, re, got, "Make with WithSuffix(6)")
	}
}

func TestMake_WithSuffix_UppercaseAlphabet(t *testing.T) {
	// With lowercasing disabled, the suffix alphabet includes A-Z.
	re := regexp.MustCompile(`^Hello-World-[a-zA-Z0-9]{8}$`)
	got := slug.Make("Hello World", slug.WithLowercase(false), slug.WithSuffix(8))
	assert.Regexp(t, re, got)
}

func TestMake_WithSuffix_Varies(t *testing.T) {
	// Random suffixes should differ across calls (astronomically unlikely to collide).
	a := slug.Make("post", slug.WithSuffix(10))
	b := slug.Make("post", slug.WithSuffix(10))
	require.NotEqual(t, a, b)
}

func TestMake_WithSuffix_EmptyBase(t *testing.T) {
	// No sluggable base + a suffix ⇒ the suffix alone (no leading separator).
	re := regexp.MustCompile(`^[a-z0-9]{5}$`)
	got := slug.Make("你好", slug.WithSuffix(5))
	assert.Regexp(t, re, got)
}
