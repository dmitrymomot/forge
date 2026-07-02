package slug_test

import (
	"testing"

	"github.com/dmitrymomot/forge/slug"
)

func BenchmarkMake(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = slug.Make("Hello, World! This is a Typical Blog Post Title")
	}
}

func BenchmarkMakeUnicode(b *testing.B) {
	// Unicode-heavy input exercises the NFKD folding path.
	b.ReportAllocs()
	for b.Loop() {
		_ = slug.Make("Crème Brûlée & Café — Ñoño Über Straße 日本語")
	}
}

func BenchmarkMakeWithOptions(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = slug.Make("Hello, World! This is a Typical Blog Post Title",
			slug.WithMaxLength(40),
			slug.WithSuffix(6),
			slug.WithSeparator("_"),
			slug.WithLowercase(true),
		)
	}
}

func BenchmarkUnique(b *testing.B) {
	// Small predicate: the base and its first two increments already exist, so
	// Unique must probe through to "-4".
	taken := map[string]bool{
		"typical-blog-post-title":   true,
		"typical-blog-post-title-2": true,
		"typical-blog-post-title-3": true,
	}
	exists := func(candidate string) bool { return taken[candidate] }
	b.ReportAllocs()
	for b.Loop() {
		_ = slug.Unique("Typical Blog Post Title", exists)
	}
}
