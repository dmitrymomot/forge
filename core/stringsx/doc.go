// Package stringsx provides string-shaping helpers stdlib lacks: case
// conversion (ToSnake/ToCamel/ToKebab), rune-safe Truncate/Ellipsis/
// TruncateWords, a last-N-runes Mask, and a naive English Pluralize.
//
// stringsx is for TRUSTED, developer-facing strings. Untrusted input belongs to
// the sanitize package; locale-aware pluralization belongs to the future i18n
// package (multi-language + custom plural rules) — do not reach for Pluralize
// when you need real localization.
//
// # Usage
//
//	stringsx.ToSnake("UserID")               // "user_id"
//	stringsx.ToCamelWith("user_id", "ID")     // "userID"
//	stringsx.Ellipsis("abcdef", 3)            // "abc…"
//	stringsx.Mask("secret123", 3)             // "******123"
//	stringsx.Pluralize("box", 2)              // "boxes"
package stringsx
