// Package stringsx provides string-shaping helpers stdlib lacks: case
// conversion (ToSnake/ToCamel/ToKebab), rune-safe Truncate/Ellipsis/
// TruncateWords, PII Mask, and a naive English Pluralize.
//
// stringsx is for TRUSTED, developer-facing strings. Untrusted input belongs to
// the sanitize package; locale-aware pluralization belongs to the future i18n
// package (multi-language + custom plural rules) — do not reach for Pluralize
// when you need real localization.
package stringsx
