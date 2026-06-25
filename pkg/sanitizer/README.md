# sanitizer

Input sanitization and data-cleaning utilities for web applications: string normalization, format-specific cleaning (email, phone, URL, credit card, SSN, postal codes), HTML/XSS hardening, collection helpers, and struct-tag-driven sanitization.

Sanitization is not validation. Use this package to clean and normalize input; use `pkg/validator` to reject it. For SQL, always prefer parameterized queries over the escaping helpers here.

## Install

```go
import "github.com/dmitrymomot/forge/pkg/sanitizer"
```

## HTML and XSS

The HTML path is backed by [bluemonday](https://github.com/microcosm-cc/bluemonday), not home-grown regexes.

```go
// Strip all HTML, returns plain text. Use for fields that must never contain markup.
plain := sanitizer.StripHTML(`<p>Hi</p><script>alert(1)</script>`) // "Hi"

// Keep a small allowlist of safe formatting tags (p, br, strong, em, lists,
// code, blockquote, a[href] with rel=nofollow).
safe := sanitizer.SanitizeHTML(`<p onclick="x()"><strong>Hi</strong></p>`) // "<p><strong>Hi</strong></p>"

// PreventXSS removes all HTML (delegates to StripHTML).
clean := sanitizer.PreventXSS(`<img src=x onerror=alert(1)>`) // ""

// EscapeHTML escapes rather than strips, for display as literal text.
esc := sanitizer.EscapeHTML("<b>") // "&lt;b&gt;"

// For a custom allowlist, pass your own bluemonday policy.
out := sanitizer.SanitizeHTMLCustom(input, policy)
```

The regex-based `SanitizeHTMLAttributes`, `StripScriptTags`, and `RemoveJavaScriptEvents` are retained for explicit composition but have known bypasses; prefer the bluemonday-backed functions above for XSS prevention.

## Struct tags

`SanitizeStruct` mutates exported string (and `*string`, `[]string`, nested struct) fields in place based on `sanitize` tags. Nested structs and non-nil struct pointers are always recursed, with or without a tag.

```go
type User struct {
    Name     string   `sanitize:"trim;title"`
    Email    string   `sanitize:"email"`
    Username string   `sanitize:"trim;lower;alphanum;max:20"`
    Bio      string   `sanitize:"trim;safe_html"`
    Tags     []string `sanitize:"trim;lower"`
    Password string   `sanitize:"-"` // "-" skips the field
}

err := sanitizer.SanitizeStruct(&user) // pass a pointer to a struct
```

Tags are separated by `;` and parameters use `:` (e.g. `max:20`), matching the
project-wide convention shared with `pkg/validator`.

### Tags

- String: `trim`, `lower`, `upper`, `title`, `trim_lower`, `trim_upper`, `kebab`, `snake`, `camel`, `single_line`, `no_spaces`, `alphanum`, `alpha`, `digits`, `max:N`
- Format: `email`, `phone`, `url`, `domain`, `credit_card`, `ssn`, `postal_code`, `filename`, `whitespace`
- Security: `escape_html`, `unescape_html`, `xss`, `html`, `strip_html`, `sql_string`, `sql_identifier`, `path`, `path_traversal`, `shell_arg`, `no_null`, `no_control`, `user_input`, `secure_filename`, `header`
- Composite: `username`, `slug`, `name`, `text`, `safe_text` (escapes HTML), `safe_html` (bluemonday `SanitizeHTML`, keeps safe formatting)

### Custom sanitizers

```go
sanitizer.RegisterSanitizer("remove_emoji", removeEmoji)
sanitizer.UnregisterSanitizer("remove_emoji") // e.g. in test cleanup
```

The registry is process-global and guarded by a `sync.RWMutex`.

## Functional composition

```go
clean := sanitizer.Compose(sanitizer.Trim, sanitizer.ToLower, sanitizer.ToKebabCase)
slug := clean("  Hello World  ") // "hello-world"

out := sanitizer.Apply("  HELLO  ", sanitizer.Trim, sanitizer.ToLower) // "hello"
```

## Collections

Helpers for cleaning slices and maps: `FilterEmpty`, `Deduplicate`, `LimitSliceLength`, `CleanStringSlice`, `SanitizeMapKeys`, `SanitizeMapValues`, `CleanStringMap`, `MergeStringMaps`, and more.

Note: `FilterSliceByPattern`, `FilterMapByKeys`, and `FilterMapByValues` use
**exclusion** (deny-list) semantics — they remove entries that contain the
pattern (case-insensitive substring) and keep the rest.

## Notes

- `SanitizeStruct` uses reflection; all other functions are plain string/slice/map operations.
- Length-limiting helpers (`MaxLength`, `LimitLength`, `SanitizeFilename`) truncate on runes, never splitting a multibyte UTF-8 character.
- Format helpers preserve the original input when it doesn't match the expected shape, to avoid silent data loss.
