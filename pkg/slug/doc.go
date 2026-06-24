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
//		slug.WithMaxLength(20),
//		slug.WithSuffix(6),
//	)
//	// Output: "long-article-x3k7f9"
//
// # Configuration Options
//
// WithMaxLength limits the slug length (rune-based):
//
//	slug.Make("Very long title", slug.WithMaxLength(15))
//	// Output: "very-long-title"
//
// WithMinLength sets the minimum slug length, padding with a random suffix if needed:
//
//	slug.Make("hi", slug.WithMinLength(10))
//	// Output: "hi-a3f7k2m9" (padded to reach 10 runes)
//
// WithSeparator sets the character used between words:
//
//	slug.Make("Product Name", slug.WithSeparator("_"))
//	// Output: "product_name"
//
// WithLowercase controls case conversion:
//
//	slug.Make("Product Name", slug.WithLowercase(false))
//	// Output: "Product-Name"
//
// WithStripChars removes specific characters before processing:
//
//	slug.Make("Price: $100", slug.WithStripChars("$:"))
//	// Output: "price-100"
//
// WithCustomReplace applies string replacements before slugification:
//
//	replacements := map[string]string{"&": "and", "@": "at"}
//	slug.Make("Fish & Chips @ Home", slug.WithCustomReplace(replacements))
//	// Output: "fish-and-chips-at-home"
//
// WithSuffix adds a random alphanumeric suffix for uniqueness:
//
//	slug.Make("Article Title", slug.WithSuffix(8))
//	// Output: "article-title-a3f7k2m9"
//
// WithReservedSlugs prevents use of specified slugs (case-insensitive) by appending a suffix:
//
//	slug.Make("admin", slug.WithReservedSlugs("admin", "api", "system"))
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
// A few ligatures and special letters are intentionally simplified to a SINGLE
// ASCII character (favoring compact slugs) rather than the conventional
// two-character transliteration. This may surprise callers who expect "ss"/"ae":
//
//	slug.Make("straße")            // "strase"  (ß -> s, not "ss")
//	slug.Make("Æsir")              // "asir"  (Æ -> a, not "ae")
//	slug.Make("œuvre")             // "ouvre"  (œ -> o, not "oe")
//	slug.Make("søster")            // "soster"  (ø -> o, not "oe")
//
// If you need the two-character forms, apply WithCustomReplace before
// slugification to override the default mapping:
//
//	slug.Make("straße", slug.WithCustomReplace(map[string]string{"ß": "ss"}))
//	// Output: "strasse"
//
// Unsupported character sets (Cyrillic, CJK, etc.) are replaced with separators.
package slug
