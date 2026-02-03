// Package slug generates URL-safe slugs from arbitrary strings with Unicode normalization.
//
// This package converts text to web-friendly identifiers by normalizing diacritics,
// replacing special characters with separators, and offering configurable options
// for length limits and collision-resistant suffixes.
//
// Basic usage:
//
//	import "github.com/dmitrymomot/forge/pkg/slug"
//
//	// Simple slug generation
//	s := slug.Make("Hello, World!")
//	// Output: "hello-world"
//
//	// With Unicode normalization
//	s = slug.Make("Café & Restaurant")
//	// Output: "cafe-restaurant"
//
//	// With configuration options
//	s = slug.Make("Long Article Title",
//		slug.MaxLength(20),
//		slug.WithSuffix(6),
//	)
//	// Output: "long-article-x3k7f9"
//
// # Configuration Options
//
// MaxLength limits the slug length (rune-based):
//
//	slug.Make("Very long title", slug.MaxLength(15))
//	// Output: "very-long-title"
//
// MinLength sets the minimum slug length, padding with a random suffix if needed:
//
//	slug.Make("hi", slug.MinLength(10))
//	// Output: "hi-a3f7k2m9" (padded to reach 10 runes)
//
// Separator sets the character used between words:
//
//	slug.Make("Product Name", slug.Separator("_"))
//	// Output: "product_name"
//
// Lowercase controls case conversion:
//
//	slug.Make("Product Name", slug.Lowercase(false))
//	// Output: "Product-Name"
//
// StripChars removes specific characters before processing:
//
//	slug.Make("Price: $100", slug.StripChars("$:"))
//	// Output: "price-100"
//
// CustomReplace applies string replacements before slugification:
//
//	replacements := map[string]string{"&": "and", "@": "at"}
//	slug.Make("Fish & Chips @ Home", slug.CustomReplace(replacements))
//	// Output: "fish-and-chips-at-home"
//
// WithSuffix adds a random alphanumeric suffix for uniqueness:
//
//	slug.Make("Article Title", slug.WithSuffix(8))
//	// Output: "article-title-a3f7k2m9"
//
// ReservedSlugs prevents use of specified slugs (case-insensitive) by appending a suffix:
//
//	slug.Make("admin", slug.ReservedSlugs("admin", "api", "system"))
//	// Output: "admin-k7x2m4" (suffix added to avoid reserved slug)
//
// # Unicode Support
//
// The package normalizes common Latin diacritics to ASCII equivalents:
//
//	slug.Make("München straße")    // "munchen-strase"
//	slug.Make("naïve résumé")      // "naive-resume"
//	slug.Make("Ñoño español")      // "nono-espanol"
//
// Unsupported character sets (Cyrillic, CJK, etc.) are replaced with separators.
package slug
