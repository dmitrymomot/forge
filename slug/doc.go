// Package slug generates URL-safe slugs from arbitrary Unicode text: it folds
// letters to ASCII (NFKD decomposition + a small special-case map), collapses
// every other run to a single separator, and offers a functional-options API for
// length, case, separator, character stripping, custom replacement, random
// suffixes, and reserved-word avoidance. Unique layers predicate-based, human-
// friendly "-2"/"-3" collision resolution on top.
//
//	slug.Make("Héllo, World!")                       // "hello-world"
//	slug.Make("admin", slug.WithReservedSlugs("admin")) // "admin-<random>"
//	slug.Unique("post", exists)                      // "post", "post-2", "post-3", …
//
// slug is NOT a transliterator: non-Latin scripts (CJK, Arabic, Cyrillic) that
// have no ASCII fold collapse to "" — callers fall back to an id. It is NOT a
// sanitizer (see the sanitize package for trust-boundary text normalization) and
// does NOT import it; per-language transliteration (MakeLang) is deliberately out
// of scope. Random suffixes come from the random package (bias-free), not a local
// crypto/rand.
package slug
