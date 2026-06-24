# randomname

Human-readable random name generation from curated word lists, using cryptographically secure randomness.

The package combines words from categories (adjectives, colors, nouns, sizes, origins, actions) with an optional hex or numeric suffix to produce names suitable for usernames, resource identifiers, project names, or display names. All randomness comes from `crypto/rand`, and selection uses rejection sampling so it is free of modulo bias.

This produces human-readable display names, not unique IDs. For IDs, use `github.com/dmitrymomot/forge/pkg/id`. Add a suffix (`Hex6`/`Hex8`/`Numeric4`) when you need collision resistance.

## Install

```go
import "github.com/dmitrymomot/forge/pkg/randomname"
```

## Usage

```go
name := randomname.Simple() // "happy-elephant" (adjective-noun)
```

### Convenience constructors

Each returns a `string` and uses the default `-` separator:

| Function | Pattern | Example |
|----------|---------|---------|
| `Simple()` | adjective-noun | `happy-elephant` |
| `Colorful()` | color-noun | `blue-whale` |
| `Descriptive()` | adjective-color-noun | `tiny-red-fox` |
| `Sized()` | size-noun | `large-dolphin` |
| `Complex()` | size-adjective-noun | `small-quick-rabbit` |
| `Full()` | size-adjective-color-noun | `huge-gentle-green-turtle` |
| `WithSuffix()` | adjective-noun-hex6 | `brave-lion-a3f2d1` |

### Generate with options

```go
name := randomname.Generate(&randomname.Options{
    Pattern:   []randomname.WordType{randomname.Color, randomname.Adjective, randomname.Noun},
    Separator: "_",
    Suffix:    randomname.Numeric4,
})
// "blue_happy_elephant_1234"
```

`Generate(nil)` is equivalent to `Simple()`. `Generate` always returns a valid name and never returns an error.

## Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Pattern` | `[]WordType` | `[Adjective, Noun]` | Word types to combine, in order. An empty pattern falls back to the default. |
| `Separator` | `string` | `-` | Joins words and suffix. An empty string falls back to `-`. |
| `Suffix` | `SuffixType` | `NoSuffix` | Suffix appended for collision avoidance. |
| `Words` | `map[WordType][]string` | — | Custom words, merged with (not replacing) the defaults for each type. |
| `Validator` | `func(string) bool` | — | Return `true` to accept a generated name, `false` to retry (up to 100 attempts). |

### Word types

`Adjective`, `Color`, `Noun`, `Size`, `Origin`, `Action`. Word types not listed in `Pattern` are simply unused. An unknown/unavailable word type in a pattern is skipped; if no pattern entry yields words, generation falls back to the default `adjective-noun` pattern (custom `Words`, separator, suffix, and validator are preserved across that fallback).

### Suffix types

| Suffix | Output | Example |
|--------|--------|---------|
| `NoSuffix` | none | — |
| `Hex6` | 6 hex chars | `a3f2d1` |
| `Hex8` | 8 hex chars | `a3f2d19b` |
| `Numeric4` | 4-digit number (1000–9999) | `4829` |

### Custom words

Custom words are appended to the defaults for that type, so both can appear:

```go
name := randomname.Generate(&randomname.Options{
    Words: map[randomname.WordType][]string{
        randomname.Adjective: {"awesome", "fantastic"},
        randomname.Noun:      {"project", "service"},
    },
})
```

### Validation with retry

```go
name := randomname.Generate(&randomname.Options{
    Validator: func(name string) bool {
        return strings.ContainsRune("aeiou", rune(strings.ToLower(name)[0]))
    },
})
// Retries up to 100 times; returns the last generated name if none pass.
```

## Notes

- All functions are stateless and safe for concurrent use.
- Randomness is sourced exclusively from `crypto/rand` with bias-free rejection sampling. There is no predictable (e.g. time-based) fallback: if entropy is unavailable the selection retries, so every returned value is derived from `crypto/rand`.
- Names are not guaranteed unique. Use a suffix (and/or a `Validator` that checks your store) when uniqueness matters.
