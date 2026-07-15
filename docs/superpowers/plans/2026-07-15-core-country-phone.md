# core/country + core/phone Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `core/country` (curated ISO-3166 static data + supported-countries `Set`) and `core/phone` (E.164 normalize/decompose value type + configured `Parser`), modeled on the `core/money` precedent.

**Architecture:** `country` is a zero-dep static-data package: a `Country` value type, ~249 exported vars, map-backed lookups, and a `Set` policy value. `phone` depends on `country`: a pointer-free `Phone` value type parsed against country's dial-code table, plus a `New(...Option)` `Parser` adding a default region and a supported-countries gate. Both use only the stdlib (+ testify in tests).

**Tech Stack:** Go 1.26, stdlib, `github.com/stretchr/testify` (tests). Spec: [docs/superpowers/specs/2026-07-15-core-country-phone-design.md](../specs/2026-07-15-core-country-phone-design.md).

## Global Constraints

- Module path `github.com/dmitrymomot/forge`; Go 1.26; single module, no sub-modules.
- `core/country`: stdlib only, no forge deps. `core/phone`: may import `core/country` only.
- Black-box tests only: test packages are `country_test` / `phone_test`, importing the package under test.
- Tests use testify (`assert`/`require`), matching `core/money`.
- Error sentinels are `errors.Is`-matchable, single-line, prefixed `country: ` / `phone: `.
- Options are `type Option func(*config)` over an unexported `config` — never builders.
- No manual line-wrapping in prose/comments/commit bodies; one line per paragraph, let it soft-wrap.
- After editing files in a package, run `just fmt ./core/<pkg>/...` (single-file `just fmt` trips a spurious betteralign error — always pass the package glob).
- After each package is complete, `just lint` must pass clean.
- Every package ships `bench_test.go` plus a measured optimization pass (before/after numbers) — required for the PR.
- Do NOT add any Claude/Anthropic attribution to commits.

---

## File Structure

**`core/country/`**
- `doc.go` — package doc + runnable example.
- `country.go` — `Country` type, `flagEmoji`, the lookups (`ByAlpha2`/`ByAlpha3`/`ByNumeric`/`ByDialCode`/`All`), and the `init` that fills emoji + builds indexes.
- `data.go` — the ~249 exported `Country` vars and the `all` pointer slice (generated static data with a provenance header).
- `set.go` — the `Set` policy type.
- `errors.go` — `ErrUnknownCode`.
- `country_test.go`, `set_test.go`, `bench_test.go`.

**`core/phone/`**
- `doc.go` — package doc + runnable example.
- `phone.go` — `Phone` type, `Parse`/`ParseRegion` free funcs, decomposition methods (`E164`/`DialCode`/`NationalNumber`/`Country`/`Candidates`/`IsZero`), `matchDial`, `toDigits`, `build`, `primaryDial`.
- `parser.go` — `Parser`, `New`, `gatePass`.
- `config.go` — unexported `config`.
- `options.go` — `Option`, `WithDefaultRegion`, `WithAllowedCountries`.
- `sql.go` — `Value`/`Scan`.
- `json.go` — `MarshalJSON`/`UnmarshalJSON`.
- `errors.go` — the four sentinels.
- `phone_test.go`, `parser_test.go`, `sql_test.go`, `json_test.go`, `bench_test.go`.

---

## Task 1: `country.Country` type, seed data, and lookups

**Files:**
- Create: `core/country/country.go`, `core/country/data.go`, `core/country/errors.go`
- Test: `core/country/country_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Country struct { Alpha2, Alpha3, Numeric, Name, Currency, DialCode, Emoji string }`
  - `func ByAlpha2(code string) (Country, bool)`
  - `func ByAlpha3(code string) (Country, bool)`
  - `func ByNumeric(code string) (Country, bool)`
  - `func ByDialCode(code string) []Country` (internal shared slice, read-only; `nil` if none)
  - `func All() []Country` (fresh slice, sorted by `Name`)
  - Exported vars for the seed set: `US CA GB DE FR AU JP IN BR ZA UA NO`
  - `var ErrUnknownCode = errors.New("country: unknown code")`

- [ ] **Step 1: Write the failing test**

`core/country/country_test.go`:
```go
package country_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/country"
)

func TestByAlpha2_HitAndCaseInsensitive(t *testing.T) {
	c, ok := country.ByAlpha2("us")
	assert.True(t, ok)
	assert.Equal(t, "US", c.Alpha2)
	assert.Equal(t, "USA", c.Alpha3)
	assert.Equal(t, "840", c.Numeric)
	assert.Equal(t, "United States", c.Name)
	assert.Equal(t, "USD", c.Currency)
	assert.Equal(t, "1", c.DialCode)
	assert.Equal(t, "\U0001F1FA\U0001F1F8", c.Emoji) // 🇺🇸
}

func TestByAlpha2_Miss(t *testing.T) {
	_, ok := country.ByAlpha2("ZZ")
	assert.False(t, ok)
}

func TestByAlpha3AndNumeric(t *testing.T) {
	c, ok := country.ByAlpha3("deu")
	assert.True(t, ok)
	assert.Equal(t, "DE", c.Alpha2)
	c, ok = country.ByNumeric("826")
	assert.True(t, ok)
	assert.Equal(t, "GB", c.Alpha2)
}

func TestByDialCode_Shared(t *testing.T) {
	cs := country.ByDialCode("1")
	codes := make([]string, len(cs))
	for i, c := range cs {
		codes[i] = c.Alpha2
	}
	assert.Contains(t, codes, "US")
	assert.Contains(t, codes, "CA")
	assert.Nil(t, country.ByDialCode("999"))
}

func TestAll_SortedByName(t *testing.T) {
	all := country.All()
	assert.NotEmpty(t, all)
	assert.True(t, sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Name < all[j].Name }))
}

func TestVars_Populated(t *testing.T) {
	assert.Equal(t, "GB", country.GB.Alpha2)
	assert.Equal(t, "\U0001F1E9\U0001F1EA", country.DE.Emoji) // 🇩🇪 filled at init
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/country/ -run 'TestBy|TestAll|TestVars' -v`
Expected: FAIL — package `country` does not exist / undefined symbols.

- [ ] **Step 3: Write the implementation**

`core/country/errors.go`:
```go
package country

import "errors"

// ErrUnknownCode is returned by NewSetFromCodes when a supplied alpha-2 code is
// not present in the bundled ISO-3166-1 table.
var ErrUnknownCode = errors.New("country: unknown code")
```

`core/country/country.go`:
```go
package country

import (
	"sort"
	"strings"
)

// Country is one ISO-3166-1 entry: its alpha-2 (the canonical key), alpha-3, and
// numeric-3 codes, English short name, primary official ISO-4217 currency code,
// E.164 dial code (no leading +), and flag emoji.
type Country struct {
	Alpha2   string
	Alpha3   string
	Numeric  string
	Name     string
	Currency string
	DialCode string
	Emoji    string
}

var (
	byAlpha2  = map[string]Country{}
	byAlpha3  = map[string]Country{}
	byNumeric = map[string]Country{}
	byDial    = map[string][]Country{}
	sorted    []Country
)

func init() {
	for _, c := range all {
		c.Emoji = flagEmoji(c.Alpha2)
	}
	sorted = make([]Country, 0, len(all))
	for _, c := range all {
		byAlpha2[c.Alpha2] = *c
		byAlpha3[c.Alpha3] = *c
		byNumeric[c.Numeric] = *c
		byDial[c.DialCode] = append(byDial[c.DialCode], *c)
		sorted = append(sorted, *c)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for k := range byDial {
		s := byDial[k]
		sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
	}
}

// flagEmoji derives a flag from an alpha-2 code by mapping each letter to its
// Unicode regional-indicator symbol. It returns "" for non-two-letter or
// non-A–Z input.
func flagEmoji(alpha2 string) string {
	if len(alpha2) != 2 {
		return ""
	}
	a, b := alpha2[0], alpha2[1]
	if a < 'A' || a > 'Z' || b < 'A' || b > 'Z' {
		return ""
	}
	const base = 0x1F1E6
	return string([]rune{rune(base + int(a-'A')), rune(base + int(b-'A'))})
}

// ByAlpha2 looks up a country by ISO-3166-1 alpha-2 code, case-insensitively.
func ByAlpha2(code string) (Country, bool) {
	c, ok := byAlpha2[strings.ToUpper(code)]
	return c, ok
}

// ByAlpha3 looks up a country by ISO-3166-1 alpha-3 code, case-insensitively.
func ByAlpha3(code string) (Country, bool) {
	c, ok := byAlpha3[strings.ToUpper(code)]
	return c, ok
}

// ByNumeric looks up a country by ISO-3166-1 numeric-3 code.
func ByNumeric(code string) (Country, bool) {
	c, ok := byNumeric[code]
	return c, ok
}

// ByDialCode returns every country sharing an E.164 dial code (many share "1").
// The returned slice is shared internal state sorted by Name and must not be
// modified; it is nil when no country uses the code.
func ByDialCode(code string) []Country {
	return byDial[code]
}

// All returns every bundled country sorted by Name. The returned slice is a
// fresh copy the caller may retain and modify.
func All() []Country {
	out := make([]Country, len(sorted))
	copy(out, sorted)
	return out
}
```

`core/country/data.go` (seed set — expanded to the full table in Task 3):
```go
package country

// ISO-3166-1 static data: alpha-2/alpha-3/numeric codes, English short name,
// primary official ISO-4217 currency, and E.164 dial code. Curated static data
// committed to the repo — no runtime fetch. Emoji flags are derived from the
// alpha-2 pair at package init. Source: ISO-3166-1 (2026 edition).

var (
	AU = Country{Alpha2: "AU", Alpha3: "AUS", Numeric: "036", Name: "Australia", Currency: "AUD", DialCode: "61"}
	BR = Country{Alpha2: "BR", Alpha3: "BRA", Numeric: "076", Name: "Brazil", Currency: "BRL", DialCode: "55"}
	CA = Country{Alpha2: "CA", Alpha3: "CAN", Numeric: "124", Name: "Canada", Currency: "CAD", DialCode: "1"}
	DE = Country{Alpha2: "DE", Alpha3: "DEU", Numeric: "276", Name: "Germany", Currency: "EUR", DialCode: "49"}
	FR = Country{Alpha2: "FR", Alpha3: "FRA", Numeric: "250", Name: "France", Currency: "EUR", DialCode: "33"}
	GB = Country{Alpha2: "GB", Alpha3: "GBR", Numeric: "826", Name: "United Kingdom", Currency: "GBP", DialCode: "44"}
	IN = Country{Alpha2: "IN", Alpha3: "IND", Numeric: "356", Name: "India", Currency: "INR", DialCode: "91"}
	JP = Country{Alpha2: "JP", Alpha3: "JPN", Numeric: "392", Name: "Japan", Currency: "JPY", DialCode: "81"}
	NO = Country{Alpha2: "NO", Alpha3: "NOR", Numeric: "578", Name: "Norway", Currency: "NOK", DialCode: "47"}
	UA = Country{Alpha2: "UA", Alpha3: "UKR", Numeric: "804", Name: "Ukraine", Currency: "UAH", DialCode: "380"}
	US = Country{Alpha2: "US", Alpha3: "USA", Numeric: "840", Name: "United States", Currency: "USD", DialCode: "1"}
	ZA = Country{Alpha2: "ZA", Alpha3: "ZAF", Numeric: "710", Name: "South Africa", Currency: "ZAR", DialCode: "27"}
)

// all is the bundled table as pointers into the exported vars, so the init emoji
// fill lands on the vars themselves. Extended to the full ISO-3166-1 set in a
// later task.
var all = []*Country{
	&AU, &BR, &CA, &DE, &FR, &GB, &IN, &JP, &NO, &UA, &US, &ZA,
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/country/ -run 'TestBy|TestAll|TestVars' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Format**

Run: `just fmt ./core/country/...`
Expected: no diff errors.

- [ ] **Step 6: Commit**

```bash
git add core/country/country.go core/country/data.go core/country/errors.go core/country/country_test.go
git commit -m "feat(core/country): Country type, seed data, and lookups"
```

---

## Task 2: `country.Set` policy type

**Files:**
- Create: `core/country/set.go`
- Test: `core/country/set_test.go`

**Interfaces:**
- Consumes: `Country`, `ByAlpha2`, `ErrUnknownCode` (Task 1).
- Produces:
  - `type Set struct { /* unexported */ }`
  - `func NewSet(cs ...Country) Set`
  - `func NewSetFromCodes(codes ...string) (Set, error)`
  - `func (s Set) Contains(c Country) bool`
  - `func (s Set) ContainsCode(code string) bool`
  - `func (s Set) All() []Country` (sorted by Name)
  - `func (s Set) Len() int`

- [ ] **Step 1: Write the failing test**

`core/country/set_test.go`:
```go
package country_test

import (
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/country"
)

func TestNewSet_ContainsAndLen(t *testing.T) {
	s := country.NewSet(country.US, country.GB, country.DE)
	assert.Equal(t, 3, s.Len())
	assert.True(t, s.Contains(country.US))
	assert.False(t, s.Contains(country.FR))
	assert.True(t, s.ContainsCode("gb"))
	assert.False(t, s.ContainsCode("fr"))
}

func TestNewSetFromCodes_OKAndFailClosed(t *testing.T) {
	s, err := country.NewSetFromCodes("US", "gb")
	require.NoError(t, err)
	assert.Equal(t, 2, s.Len())
	assert.True(t, s.ContainsCode("US"))

	_, err = country.NewSetFromCodes("US", "ZZ")
	require.Error(t, err)
	assert.ErrorIs(t, err, country.ErrUnknownCode)
}

func TestSet_AllSorted(t *testing.T) {
	s := country.NewSet(country.US, country.GB, country.DE)
	all := s.All()
	assert.Len(t, all, 3)
	assert.True(t, sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Name < all[j].Name }))
}

func TestSet_ZeroValueFailsClosed(t *testing.T) {
	var s country.Set
	assert.Equal(t, 0, s.Len())
	assert.False(t, s.Contains(country.US))
	assert.False(t, s.ContainsCode("US"))
	assert.Empty(t, s.All())
	_ = errors.Is
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/country/ -run 'TestNewSet|TestSet' -v`
Expected: FAIL — `Set`, `NewSet`, `NewSetFromCodes` undefined.

- [ ] **Step 3: Write the implementation**

`core/country/set.go`:
```go
package country

import (
	"fmt"
	"sort"
	"strings"
)

// Set is an explicit, immutable collection of countries — a consumer's
// "supported countries" policy, shared by filtered UI dropdowns and phone's
// parse gate. The zero Set is a valid empty set: it contains nothing, so a
// gate configured with it fails closed.
type Set struct {
	m map[string]struct{} // uppercase alpha-2 keys
}

// NewSet builds a Set from Country values. Zero-value countries are ignored.
func NewSet(cs ...Country) Set {
	s := Set{m: make(map[string]struct{}, len(cs))}
	for _, c := range cs {
		if c.Alpha2 != "" {
			s.m[c.Alpha2] = struct{}{}
		}
	}
	return s
}

// NewSetFromCodes builds a Set from alpha-2 code strings (the form configuration
// supplies). It fails closed: an unknown code returns the zero Set wrapping
// ErrUnknownCode.
func NewSetFromCodes(codes ...string) (Set, error) {
	s := Set{m: make(map[string]struct{}, len(codes))}
	for _, code := range codes {
		c, ok := ByAlpha2(code)
		if !ok {
			return Set{}, fmt.Errorf("country: %q: %w", code, ErrUnknownCode)
		}
		s.m[c.Alpha2] = struct{}{}
	}
	return s, nil
}

// Contains reports whether c is in the set.
func (s Set) Contains(c Country) bool {
	_, ok := s.m[c.Alpha2]
	return ok
}

// ContainsCode reports whether the alpha-2 code is in the set, case-insensitively.
func (s Set) ContainsCode(code string) bool {
	_, ok := s.m[strings.ToUpper(code)]
	return ok
}

// All returns the set's countries sorted by Name — the filtered dropdown source.
func (s Set) All() []Country {
	out := make([]Country, 0, len(s.m))
	for code := range s.m {
		if c, ok := ByAlpha2(code); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Len returns the number of countries in the set.
func (s Set) Len() int {
	return len(s.m)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/country/ -run 'TestNewSet|TestSet' -v`
Expected: PASS.

- [ ] **Step 5: Format**

Run: `just fmt ./core/country/...`

- [ ] **Step 6: Commit**

```bash
git add core/country/set.go core/country/set_test.go
git commit -m "feat(core/country): supported-countries Set policy type"
```

---

## Task 3: Populate the full ISO-3166-1 table

**Files:**
- Modify: `core/country/data.go` (extend to the full set)
- Test: `core/country/data_test.go` (invariant guard)

**Interfaces:**
- Consumes: `Country`, `All`, `flagEmoji`-equivalent formula (recomputed in the test), lookups (Task 1).
- Produces: exported vars for every ISO-3166-1 country (`country.AD` … `country.ZW`) appended to `all`; no new functions.

**Note:** This is a mechanical data fill, not logic. The invariant test below is written FIRST and guards the fill (shape, uniqueness, derived emoji, dial-code digits). Populate `data.go` from the canonical ISO-3166-1 list — one exported var per country following the exact literal form from Task 1, and add each new var's address to the `all` slice. Assigned Primary currency = the country's current official ISO-4217 code. Keep entries alphabetically ordered by alpha-2 for reviewability.

- [ ] **Step 1: Write the failing invariant test**

`core/country/data_test.go`:
```go
package country_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/country"
)

func flag(alpha2 string) string {
	const base = 0x1F1E6
	return string([]rune{rune(base + int(alpha2[0]-'A')), rune(base + int(alpha2[1]-'A'))})
}

func TestTable_Invariants(t *testing.T) {
	all := country.All()
	assert.GreaterOrEqual(t, len(all), 240, "expected the full ISO-3166-1 set")

	seenA2 := map[string]bool{}
	seenA3 := map[string]bool{}
	seenNum := map[string]bool{}
	for _, c := range all {
		assert.Len(t, c.Alpha2, 2, "alpha2 %q", c.Alpha2)
		assert.Len(t, c.Alpha3, 3, "alpha3 %q", c.Alpha3)
		assert.Len(t, c.Numeric, 3, "numeric %q for %s", c.Numeric, c.Alpha2)
		assert.NotEmpty(t, c.Name, "name for %s", c.Alpha2)
		assert.Len(t, c.Currency, 3, "currency %q for %s", c.Currency, c.Alpha2)
		assert.NotEmpty(t, c.DialCode, "dial for %s", c.Alpha2)
		for i := range len(c.DialCode) {
			assert.True(t, c.DialCode[i] >= '0' && c.DialCode[i] <= '9', "dial %q not numeric", c.DialCode)
		}
		assert.LessOrEqual(t, len(c.DialCode), 3, "dial %q too long", c.DialCode)
		assert.Equal(t, flag(c.Alpha2), c.Emoji, "emoji mismatch for %s", c.Alpha2)

		assert.False(t, seenA2[c.Alpha2], "duplicate alpha2 %s", c.Alpha2)
		assert.False(t, seenA3[c.Alpha3], "duplicate alpha3 %s", c.Alpha3)
		assert.False(t, seenNum[c.Numeric], "duplicate numeric %s", c.Numeric)
		seenA2[c.Alpha2], seenA3[c.Alpha3], seenNum[c.Numeric] = true, true, true
	}
}

func TestTable_KnownSpotChecks(t *testing.T) {
	for _, tc := range []struct{ code, name, cur, dial string }{
		{"AD", "Andorra", "EUR", "376"},
		{"CN", "China", "CNY", "86"},
		{"EG", "Egypt", "EGP", "20"},
		{"NZ", "New Zealand", "NZD", "64"},
		{"ZW", "Zimbabwe", "ZWG", "263"},
	} {
		c, ok := country.ByAlpha2(tc.code)
		assert.True(t, ok, tc.code)
		assert.Equal(t, tc.name, c.Name, tc.code)
		assert.Equal(t, tc.cur, c.Currency, tc.code)
		assert.Equal(t, tc.dial, c.DialCode, tc.code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/country/ -run TestTable -v`
Expected: FAIL — only 12 seed rows, `GreaterOrEqual 240` fails and `AD`/`CN`/etc. lookups miss.

- [ ] **Step 3: Populate `data.go` with the full ISO-3166-1 table**

Extend the `var (...)` block in `core/country/data.go` with one exported var per ISO-3166-1 country (alpha-2 order), each in the exact literal form from Task 1 (`Alpha2`/`Alpha3`/`Numeric`/`Name`/`Currency`/`DialCode`; no `Emoji` — it is filled at init). Add every new var's address to the `all` slice. The invariant test is the acceptance gate. Example additions:
```go
	AD = Country{Alpha2: "AD", Alpha3: "AND", Numeric: "020", Name: "Andorra", Currency: "EUR", DialCode: "376"}
	AE = Country{Alpha2: "AE", Alpha3: "ARE", Numeric: "784", Name: "United Arab Emirates", Currency: "AED", DialCode: "971"}
	// … through …
	ZW = Country{Alpha2: "ZW", Alpha3: "ZWE", Numeric: "716", Name: "Zimbabwe", Currency: "ZWG", DialCode: "263"}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/country/ -run TestTable -v && go test ./core/country/ -v`
Expected: PASS — table + all prior tests green.

- [ ] **Step 5: Format**

Run: `just fmt ./core/country/...`

- [ ] **Step 6: Commit**

```bash
git add core/country/data.go core/country/data_test.go
git commit -m "feat(core/country): populate full ISO-3166-1 table"
```

---

## Task 4: `country` doc, benchmarks, optimization pass, roadmap

**Files:**
- Create: `core/country/doc.go`, `core/country/bench_test.go`
- Modify: `docs/packages.md` (delete the `core/country` entry)

**Interfaces:**
- Consumes: the full `country` API (Tasks 1–3).
- Produces: no new symbols.

- [ ] **Step 1: Write benchmarks**

`core/country/bench_test.go`:
```go
package country_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/country"
)

func BenchmarkByAlpha2(b *testing.B) {
	for b.Loop() {
		_, _ = country.ByAlpha2("US")
	}
}

func BenchmarkByDialCode(b *testing.B) {
	for b.Loop() {
		_ = country.ByDialCode("1")
	}
}

func BenchmarkAll(b *testing.B) {
	for b.Loop() {
		_ = country.All()
	}
}

func BenchmarkNewSetFromCodes(b *testing.B) {
	for b.Loop() {
		_, _ = country.NewSetFromCodes("US", "GB", "DE", "FR")
	}
}
```

- [ ] **Step 2: Run the benchmarks (record before numbers)**

Run: `just bench ./core/country/...`
Expected: results print; note ns/op and allocs/op for each. `ByAlpha2` and `ByDialCode` should be 0 allocs/op; if not, that is the optimization-pass target.

- [ ] **Step 3: Write `doc.go`**

`core/country/doc.go`:
```go
// Package country provides curated ISO-3166-1 static data — alpha-2, alpha-3,
// and numeric codes, English short name, primary official ISO-4217 currency
// code, E.164 dial code, and flag emoji — plus case-insensitive lookups and a
// Set type expressing a "supported countries" policy.
//
// Every country is exposed as a package-level Country var (US, GB, DE, …) and
// indexed for lookup by ByAlpha2, ByAlpha3, ByNumeric, and ByDialCode; All
// returns the whole table sorted by Name (a dropdown source). Because many
// countries share one dial code (+1 covers the US, Canada, and the Caribbean),
// ByDialCode returns a slice. The data is committed static — no runtime fetch;
// flag emoji are derived from the alpha-2 pair at package init.
//
// A Set is an explicit, immutable collection a consumer constructs (NewSet, or
// NewSetFromCodes over configuration strings, which fails closed on an unknown
// code) and passes wherever a supported-countries policy is needed — the
// filtered All dropdown, or core/phone's parse gate. The zero Set is a valid
// empty set that contains nothing, so a gate configured with it fails closed.
//
// What this is NOT: it carries only the primary official currency per country
// (not de-facto multi-currency reality), no ISO-3166-2 subdivisions/states, no
// translated names (that is an i18n concern), and no historical or deprecated
// codes. Cheap shape-only validation of a country code lives in core/validate;
// this package is the authoritative data.
//
// # Usage
//
//	c, ok := country.ByAlpha2("us")
//	if ok {
//		_ = c.Name     // "United States"
//		_ = c.Currency // "USD"
//		_ = c.Emoji    // "🇺🇸"
//	}
//
//	supported, _ := country.NewSetFromCodes("US", "GB", "DE")
//	_ = supported.ContainsCode("fr") // false
//	_ = supported.All()              // sorted, for a filtered dropdown
package country
```

- [ ] **Step 4: Optimization pass (only if a benchmark showed a win)**

If Step 2 showed non-zero allocs on `ByAlpha2`/`ByDialCode`, or an obvious hot-path win, apply the minimal change and re-run `just bench ./core/country/...`. Record before/after in the PR description. If already optimal (0 allocs on the lookups), note "no change — lookups already 0 allocs/op" in the PR. Make no speculative changes.

- [ ] **Step 5: Delete the roadmap entry**

In `docs/packages.md`, remove the `**core/country**` block (heading through its `Deps:` line and the surrounding `---` separators for that entry only). Leave `core/phone` until Task 9.

- [ ] **Step 6: Verify and commit**

Run: `just lint`
Expected: clean (vet, build, golangci-lint, nilaway, betteralign, modernize all pass).
```bash
git add core/country/doc.go core/country/bench_test.go docs/packages.md
git commit -m "docs(core/country): doc.go, benchmarks, drop roadmap entry"
```

---

## Task 5: `phone.Phone` type and `Parse`

**Files:**
- Create: `core/phone/phone.go`, `core/phone/errors.go`
- Test: `core/phone/phone_test.go`

**Interfaces:**
- Consumes: `country.ByDialCode`, `country.ByAlpha2` (country package).
- Produces:
  - `type Phone struct { /* unexported: e164 string, dialLen int, resolved string */ }`
  - `func Parse(input string) (Phone, error)`
  - `func (p Phone) E164() string`
  - `func (p Phone) DialCode() string`
  - `func (p Phone) NationalNumber() string`
  - `func (p Phone) IsZero() bool`
  - internal: `toDigits(input string, requireCC bool) (string, error)`, `matchDial(digits string) (string, bool)`, `build(digits, resolved string) (Phone, error)`
  - `var ErrInvalidNumber, ErrMissingCountryCode, ErrUnknownDialCode, ErrUnsupportedRegion`

- [ ] **Step 1: Write the failing test**

`core/phone/phone_test.go`:
```go
package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/phone"
)

func TestParse_PlusForm(t *testing.T) {
	p, err := phone.Parse("+1 (415) 555-2671")
	require.NoError(t, err)
	assert.Equal(t, "+14155552671", p.E164())
	assert.Equal(t, "1", p.DialCode())
	assert.Equal(t, "4155552671", p.NationalNumber())
	assert.False(t, p.IsZero())
}

func TestParse_DoubleZeroPrefix(t *testing.T) {
	p, err := phone.Parse("0044 20 7946 0018")
	require.NoError(t, err)
	assert.Equal(t, "+442079460018", p.E164())
	assert.Equal(t, "44", p.DialCode())
}

func TestParse_Errors(t *testing.T) {
	_, err := phone.Parse("415-555-2671")
	assert.ErrorIs(t, err, phone.ErrMissingCountryCode)

	_, err = phone.Parse("+1 abc")
	assert.ErrorIs(t, err, phone.ErrInvalidNumber)

	_, err = phone.Parse("+999 12345")
	assert.ErrorIs(t, err, phone.ErrUnknownDialCode)

	_, err = phone.Parse("+1")
	assert.ErrorIs(t, err, phone.ErrInvalidNumber) // national number empty

	_, err = phone.Parse("+1 1234567890123456")
	assert.ErrorIs(t, err, phone.ErrInvalidNumber) // > 15 digits
}

func TestParse_ZeroValue(t *testing.T) {
	var p phone.Phone
	assert.True(t, p.IsZero())
	assert.Empty(t, p.E164())
	assert.Empty(t, p.DialCode())
	assert.Empty(t, p.NationalNumber())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/phone/ -run TestParse -v`
Expected: FAIL — package `phone` undefined.

- [ ] **Step 3: Write the implementation**

`core/phone/errors.go`:
```go
package phone

import "errors"

// ErrInvalidNumber is returned when input contains non-digit content or its
// digit count is outside E.164 bounds (empty national number, or over 15 total).
var ErrInvalidNumber = errors.New("phone: invalid number")

// ErrMissingCountryCode is returned by Parse when input has no leading + or 00
// and no region context supplies a dial code.
var ErrMissingCountryCode = errors.New("phone: missing country code")

// ErrUnknownDialCode is returned when the leading digits match no country
// calling code in the bundled table.
var ErrUnknownDialCode = errors.New("phone: unknown dial code")

// ErrUnsupportedRegion is returned by a gated Parser when the resolved country
// is provably outside the configured supported-countries Set.
var ErrUnsupportedRegion = errors.New("phone: unsupported region")
```

`core/phone/phone.go`:
```go
package phone

import (
	"strings"

	"github.com/dmitrymomot/forge/core/country"
)

// Phone is a normalized E.164 phone number. The zero Phone is the empty value
// (IsZero reports true). It is pointer-free for low GC scan cost.
type Phone struct {
	e164     string // "+14155552671"
	resolved string // alpha-2 when a region hint pinned the country; "" otherwise
	dialLen  int    // dial-code digit count, for splitting E164 into dial + national
}

// primaryDial designates the "main" country for a shared dial code, so Country
// can return a stable primary for an ambiguous number. Codes not listed fall
// back to the first candidate by Name.
var primaryDial = map[string]string{
	"1": "US", "7": "RU", "44": "GB", "39": "IT", "61": "AU", "47": "NO", "212": "MA", "358": "FI",
}

// Parse normalizes a phone number that carries its own country code (a leading +
// or 00). Formatting characters (spaces, dashes, parentheses, dots, slashes) are
// stripped. It returns ErrMissingCountryCode when no + or 00 is present.
func Parse(input string) (Phone, error) {
	digits, err := toDigits(input, true)
	if err != nil {
		return Phone{}, err
	}
	return build(digits, "")
}

// toDigits strips a leading + or 00 and all formatting separators, returning the
// bare digit string. When requireCC is true, absence of a + or 00 is an error.
func toDigits(input string, requireCC bool) (string, error) {
	s := strings.TrimSpace(input)
	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "00"):
		s = s[2:]
	default:
		if requireCC {
			return "", ErrMissingCountryCode
		}
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		ch := s[i]
		switch {
		case ch >= '0' && ch <= '9':
			b.WriteByte(ch)
		case isSep(ch):
			// drop
		default:
			return "", ErrInvalidNumber
		}
	}
	d := b.String()
	if d == "" {
		return "", ErrInvalidNumber
	}
	return d, nil
}

func isSep(ch byte) bool {
	switch ch {
	case ' ', '-', '(', ')', '.', '/':
		return true
	}
	return false
}

// matchDial finds the longest dial-code prefix (1–3 digits) present in country's
// table.
func matchDial(digits string) (string, bool) {
	for n := 3; n >= 1; n-- {
		if len(digits) < n {
			continue
		}
		p := digits[:n]
		if len(country.ByDialCode(p)) > 0 {
			return p, true
		}
	}
	return "", false
}

// build validates E.164 length, resolves the dial code, and constructs a Phone.
// resolved, when non-empty, records a region hint that pinned the country.
func build(digits, resolved string) (Phone, error) {
	if len(digits) > 15 {
		return Phone{}, ErrInvalidNumber
	}
	dial, ok := matchDial(digits)
	if !ok {
		return Phone{}, ErrUnknownDialCode
	}
	if len(digits) <= len(dial) {
		return Phone{}, ErrInvalidNumber // national number empty
	}
	return Phone{e164: "+" + digits, resolved: resolved, dialLen: len(dial)}, nil
}

// E164 returns the canonical E.164 form, e.g. "+14155552671" ("" for the zero
// Phone).
func (p Phone) E164() string { return p.e164 }

// DialCode returns the E.164 country calling code without the +, e.g. "1".
func (p Phone) DialCode() string {
	if p.e164 == "" {
		return ""
	}
	return p.e164[1 : 1+p.dialLen]
}

// NationalNumber returns the significant number after the dial code, e.g.
// "4155552671".
func (p Phone) NationalNumber() string {
	if p.e164 == "" {
		return ""
	}
	return p.e164[1+p.dialLen:]
}

// IsZero reports whether p is the zero Phone.
func (p Phone) IsZero() bool { return p.e164 == "" }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/phone/ -run TestParse -v`
Expected: PASS.

- [ ] **Step 5: Format**

Run: `just fmt ./core/phone/...`

- [ ] **Step 6: Commit**

```bash
git add core/phone/phone.go core/phone/errors.go core/phone/phone_test.go
git commit -m "feat(core/phone): Phone type and Parse"
```

---

## Task 6: `ParseRegion`, `Country`, and `Candidates`

**Files:**
- Modify: `core/phone/phone.go` (add `ParseRegion`, `Country`, `Candidates`)
- Test: `core/phone/region_test.go`

**Interfaces:**
- Consumes: `Parse`, `build`, `toDigits`, `primaryDial` (Task 5); `country.ByAlpha2`, `country.ByDialCode`.
- Produces:
  - `func ParseRegion(input, alpha2 string) (Phone, error)`
  - `func (p Phone) Country() (country.Country, bool)`
  - `func (p Phone) Candidates() []country.Country`

- [ ] **Step 1: Write the failing test**

`core/phone/region_test.go`:
```go
package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/country"
	"github.com/dmitrymomot/forge/core/phone"
)

func TestParseRegion_BareNationalStripsTrunkZero(t *testing.T) {
	p, err := phone.ParseRegion("07911 123456", "GB")
	require.NoError(t, err)
	assert.Equal(t, "+447911123456", p.E164())
	c, ok := p.Country()
	assert.True(t, ok)
	assert.Equal(t, "GB", c.Alpha2)
}

func TestParseRegion_UnknownRegion(t *testing.T) {
	_, err := phone.ParseRegion("07911 123456", "ZZ")
	assert.ErrorIs(t, err, phone.ErrMissingCountryCode)
}

func TestParseRegion_PlusInputResolvesSharedCode(t *testing.T) {
	p, err := phone.ParseRegion("+1 604 555 0199", "CA")
	require.NoError(t, err)
	c, ok := p.Country()
	assert.True(t, ok)
	assert.Equal(t, "CA", c.Alpha2) // region hint pinned CA among +1 candidates
}

func TestCountry_UniqueDialCode(t *testing.T) {
	p, _ := phone.Parse("+44 20 7946 0018")
	c, ok := p.Country()
	assert.True(t, ok)
	assert.Equal(t, "GB", c.Alpha2)
}

func TestCountry_AmbiguousReturnsPrimaryFalse(t *testing.T) {
	p, _ := phone.Parse("+1 415 555 2671")
	c, ok := p.Country()
	assert.False(t, ok) // ambiguous +1, no hint
	assert.Equal(t, "US", c.Alpha2)

	cands := p.Candidates()
	codes := make([]string, len(cands))
	for i, c := range cands {
		codes[i] = c.Alpha2
	}
	assert.Contains(t, codes, "US")
	assert.Contains(t, codes, "CA")
}

func TestCandidates_ZeroPhone(t *testing.T) {
	var p phone.Phone
	assert.Nil(t, p.Candidates())
	_, ok := p.Country()
	assert.False(t, ok)
	_ = country.US
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/phone/ -run 'TestParseRegion|TestCountry|TestCandidates' -v`
Expected: FAIL — `ParseRegion`, `Country`, `Candidates` undefined.

- [ ] **Step 3: Add the implementation to `phone.go`**

Append to `core/phone/phone.go`:
```go
// ParseRegion parses a number using a region hint (an alpha-2 code). Bare
// national input has one leading trunk 0 stripped and the region's dial code
// prepended; input that already carries a + or 00 is parsed as-is, and the
// region, when it is among the dial code's candidates, resolves the country.
// An unknown region yields ErrMissingCountryCode.
//
// Known limitation: the single-leading-0 trunk-prefix rule is near-universal but
// not absolute (NANP uses no trunk 0; some plans keep a significant leading 0) —
// callers in those regions pass fully-qualified + input to Parse.
func ParseRegion(input, alpha2 string) (Phone, error) {
	c, ok := country.ByAlpha2(alpha2)
	if !ok {
		return Phone{}, ErrMissingCountryCode
	}
	s := strings.TrimSpace(input)
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "00") {
		p, err := Parse(s)
		if err != nil {
			return Phone{}, err
		}
		for _, cand := range p.Candidates() {
			if cand.Alpha2 == c.Alpha2 {
				p.resolved = c.Alpha2
				break
			}
		}
		return p, nil
	}
	digits, err := toDigits(s, false)
	if err != nil {
		return Phone{}, err
	}
	digits = strings.TrimPrefix(digits, "0")
	if digits == "" {
		return Phone{}, ErrInvalidNumber
	}
	return build(c.DialCode+digits, c.Alpha2)
}

// Candidates returns every country sharing this number's dial code (nil for the
// zero Phone). It is the escape hatch for the shared-dial-code case Country
// cannot disambiguate.
func (p Phone) Candidates() []country.Country {
	if p.e164 == "" {
		return nil
	}
	return country.ByDialCode(p.DialCode())
}

// Country returns the number's country. The bool is true when the country is
// certain — a dial code used by exactly one country, or a region hint that
// pinned it — and false when the dial code is shared and unresolved, in which
// case a stable primary is still returned (use Candidates for all options).
func (p Phone) Country() (country.Country, bool) {
	if p.e164 == "" {
		return country.Country{}, false
	}
	if p.resolved != "" {
		c, ok := country.ByAlpha2(p.resolved)
		return c, ok
	}
	cs := p.Candidates()
	switch len(cs) {
	case 0:
		return country.Country{}, false
	case 1:
		return cs[0], true
	default:
		if a, ok := primaryDial[p.DialCode()]; ok {
			if c, ok2 := country.ByAlpha2(a); ok2 {
				return c, false
			}
		}
		return cs[0], false
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/phone/ -run 'TestParseRegion|TestCountry|TestCandidates' -v`
Expected: PASS. (Requires `CA` in the full table from Task 3 — sharing dial code `1` with `US`.)

- [ ] **Step 5: Format**

Run: `just fmt ./core/phone/...`

- [ ] **Step 6: Commit**

```bash
git add core/phone/phone.go core/phone/region_test.go
git commit -m "feat(core/phone): ParseRegion, Country, Candidates"
```

---

## Task 7: `Parser` with default region and supported-countries gate

**Files:**
- Create: `core/phone/config.go`, `core/phone/options.go`, `core/phone/parser.go`
- Test: `core/phone/parser_test.go`

**Interfaces:**
- Consumes: `Parse`, `ParseRegion`, `Phone.Country`, `Phone.Candidates` (Tasks 5–6); `country.Set`, `country.ByAlpha2`; `ErrMissingCountryCode`, `ErrUnsupportedRegion`.
- Produces:
  - `type Option func(*config)`
  - `func WithDefaultRegion(alpha2 string) Option`
  - `func WithAllowedCountries(s country.Set) Option`
  - `type Parser struct { /* unexported */ }`
  - `func New(opts ...Option) (*Parser, error)`
  - `func (p *Parser) Parse(input string) (Phone, error)`

- [ ] **Step 1: Write the failing test**

`core/phone/parser_test.go`:
```go
package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/country"
	"github.com/dmitrymomot/forge/core/phone"
)

func TestNew_DefaultRegionValidated(t *testing.T) {
	_, err := phone.New(phone.WithDefaultRegion("ZZ"))
	require.Error(t, err)
	assert.ErrorIs(t, err, phone.ErrMissingCountryCode)
}

func TestParser_DefaultRegionAppliesToBareInput(t *testing.T) {
	p, err := phone.New(phone.WithDefaultRegion("US"))
	require.NoError(t, err)
	ph, err := p.Parse("415-555-2671")
	require.NoError(t, err)
	assert.Equal(t, "+14155552671", ph.E164())
}

func TestParser_GateRejectsUnsupported(t *testing.T) {
	set := country.NewSet(country.US, country.GB)
	p, err := phone.New(phone.WithAllowedCountries(set))
	require.NoError(t, err)

	_, err = p.Parse("+33 6 12 34 56 78") // France, unique dial code, not in set
	assert.ErrorIs(t, err, phone.ErrUnsupportedRegion)

	ph, err := p.Parse("+44 20 7946 0018") // GB, supported
	require.NoError(t, err)
	assert.Equal(t, "GB", func() string { c, _ := ph.Country(); return c.Alpha2 }())
}

func TestParser_GateAmbiguousPassesIfAnyCandidateSupported(t *testing.T) {
	set := country.NewSet(country.US) // US supported, CA not
	p, err := phone.New(phone.WithAllowedCountries(set))
	require.NoError(t, err)
	_, err = p.Parse("+1 415 555 2671") // ambiguous +1; US is a candidate → passes
	assert.NoError(t, err)
}

func TestParser_ZeroSetFailsClosed(t *testing.T) {
	var empty country.Set
	p, err := phone.New(phone.WithAllowedCountries(empty))
	require.NoError(t, err)
	_, err = p.Parse("+1 415 555 2671")
	assert.ErrorIs(t, err, phone.ErrUnsupportedRegion)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/phone/ -run 'TestNew|TestParser' -v`
Expected: FAIL — `New`, `WithDefaultRegion`, `WithAllowedCountries` undefined.

- [ ] **Step 3: Write the implementation**

`core/phone/config.go`:
```go
package phone

import "github.com/dmitrymomot/forge/core/country"

type config struct {
	defaultRegion string
	allowed       country.Set
	gate          bool
}
```

`core/phone/options.go`:
```go
package phone

import "github.com/dmitrymomot/forge/core/country"

// Option configures a Parser.
type Option func(*config)

// WithDefaultRegion sets the alpha-2 region used to interpret bare national
// input (numbers with no + or 00). New rejects an unknown region.
func WithDefaultRegion(alpha2 string) Option {
	return func(c *config) { c.defaultRegion = alpha2 }
}

// WithAllowedCountries enables the supported-countries gate: Parse rejects a
// number whose country is provably outside the set with ErrUnsupportedRegion.
// Passing the zero Set fails closed (rejects everything).
func WithAllowedCountries(s country.Set) Option {
	return func(c *config) {
		c.allowed = s
		c.gate = true
	}
}
```

`core/phone/parser.go`:
```go
package phone

import (
	"fmt"

	"github.com/dmitrymomot/forge/core/country"
)

// Parser is a configured phone parser: an optional default region for bare
// national input, and an optional supported-countries gate.
type Parser struct {
	cfg config
}

// New builds a Parser from options. It fails closed on an unknown default
// region, returning ErrMissingCountryCode.
func New(opts ...Option) (*Parser, error) {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if c.defaultRegion != "" {
		if _, ok := country.ByAlpha2(c.defaultRegion); !ok {
			return nil, fmt.Errorf("phone: unknown default region %q: %w", c.defaultRegion, ErrMissingCountryCode)
		}
	}
	return &Parser{cfg: c}, nil
}

// Parse normalizes input, applying the default region (if configured) to bare
// national numbers, then the supported-countries gate (if configured).
func (p *Parser) Parse(input string) (Phone, error) {
	var (
		ph  Phone
		err error
	)
	if p.cfg.defaultRegion != "" {
		ph, err = ParseRegion(input, p.cfg.defaultRegion)
	} else {
		ph, err = Parse(input)
	}
	if err != nil {
		return Phone{}, err
	}
	if p.cfg.gate && !gatePass(p.cfg.allowed, ph) {
		return Phone{}, ErrUnsupportedRegion
	}
	return ph, nil
}

// gatePass reports whether ph is allowed: a resolved/unique country must be in
// the set; an ambiguous number passes when any candidate is in the set (the
// number cannot be proven to belong to the unsupported one).
func gatePass(set country.Set, ph Phone) bool {
	if c, ok := ph.Country(); ok {
		return set.Contains(c)
	}
	for _, c := range ph.Candidates() {
		if set.Contains(c) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/phone/ -run 'TestNew|TestParser' -v`
Expected: PASS.

- [ ] **Step 5: Format**

Run: `just fmt ./core/phone/...`

- [ ] **Step 6: Commit**

```bash
git add core/phone/config.go core/phone/options.go core/phone/parser.go core/phone/parser_test.go
git commit -m "feat(core/phone): configured Parser with default region and supported-countries gate"
```

---

## Task 8: SQL and JSON marshaling

**Files:**
- Create: `core/phone/sql.go`, `core/phone/json.go`
- Test: `core/phone/sql_test.go`, `core/phone/json_test.go`

**Interfaces:**
- Consumes: `Phone`, `Parse` (Tasks 5).
- Produces:
  - `func (p Phone) Value() (driver.Value, error)`
  - `func (p *Phone) Scan(src any) error`
  - `func (p Phone) MarshalJSON() ([]byte, error)`
  - `func (p *Phone) UnmarshalJSON(b []byte) error`

- [ ] **Step 1: Write the failing tests**

`core/phone/sql_test.go`:
```go
package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/phone"
)

func TestValueAndScan_RoundTrip(t *testing.T) {
	p, _ := phone.Parse("+14155552671")
	v, err := p.Value()
	require.NoError(t, err)
	assert.Equal(t, "+14155552671", v)

	var got phone.Phone
	require.NoError(t, got.Scan("+14155552671"))
	assert.Equal(t, "+14155552671", got.E164())

	require.NoError(t, got.Scan([]byte("+442079460018")))
	assert.Equal(t, "+442079460018", got.E164())
}

func TestValueAndScan_ZeroAndNull(t *testing.T) {
	var zero phone.Phone
	v, err := zero.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	var got phone.Phone
	require.NoError(t, got.Scan(nil))
	assert.True(t, got.IsZero())
	require.NoError(t, got.Scan(""))
	assert.True(t, got.IsZero())
}

func TestScan_Garbage(t *testing.T) {
	var p phone.Phone
	assert.ErrorIs(t, p.Scan("nope"), phone.ErrMissingCountryCode)
	assert.ErrorIs(t, p.Scan(12345), phone.ErrInvalidNumber)
}
```

`core/phone/json_test.go`:
```go
package phone_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/phone"
)

func TestMarshalJSON_RoundTrip(t *testing.T) {
	p, _ := phone.Parse("+14155552671")
	b, err := json.Marshal(p)
	require.NoError(t, err)
	assert.JSONEq(t, `"+14155552671"`, string(b))

	var got phone.Phone
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "+14155552671", got.E164())
}

func TestMarshalJSON_ZeroIsNull(t *testing.T) {
	var zero phone.Phone
	b, err := json.Marshal(zero)
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))

	var got phone.Phone
	require.NoError(t, json.Unmarshal([]byte("null"), &got))
	assert.True(t, got.IsZero())
}

func TestUnmarshalJSON_Garbage(t *testing.T) {
	var p phone.Phone
	assert.ErrorIs(t, json.Unmarshal([]byte(`"nope"`), &p), phone.ErrMissingCountryCode)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/phone/ -run 'TestValue|TestScan|TestMarshal|TestUnmarshal' -v`
Expected: FAIL — `Value`/`Scan`/`MarshalJSON`/`UnmarshalJSON` undefined.

- [ ] **Step 3: Write the implementation**

`core/phone/sql.go`:
```go
package phone

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// Value implements driver.Valuer, storing the E.164 string. The zero Phone
// stores SQL NULL.
func (p Phone) Value() (driver.Value, error) {
	if p.e164 == "" {
		return nil, nil
	}
	return p.e164, nil
}

// Scan implements sql.Scanner for the E.164 string form. A nil or empty source
// yields the zero Phone; any other value is re-parsed by Parse.
func (p *Phone) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*p = Phone{}
		return nil
	case string:
		return p.scan(v)
	case []byte:
		return p.scan(string(v))
	default:
		return fmt.Errorf("phone: cannot scan %T: %w", src, ErrInvalidNumber)
	}
}

func (p *Phone) scan(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		*p = Phone{}
		return nil
	}
	parsed, err := Parse(s)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
```

`core/phone/json.go`:
```go
package phone

import "encoding/json"

// MarshalJSON emits the E.164 string, e.g. "+14155552671". The zero Phone emits
// JSON null.
func (p Phone) MarshalJSON() ([]byte, error) {
	if p.e164 == "" {
		return []byte("null"), nil
	}
	return json.Marshal(p.e164)
}

// UnmarshalJSON parses the E.164 string form. JSON null or an empty string sets
// the zero Phone; any other value is validated by Parse.
func (p *Phone) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*p = Phone{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*p = Phone{}
		return nil
	}
	parsed, err := Parse(s)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/phone/ -run 'TestValue|TestScan|TestMarshal|TestUnmarshal' -v`
Expected: PASS.

- [ ] **Step 5: Format**

Run: `just fmt ./core/phone/...`

- [ ] **Step 6: Commit**

```bash
git add core/phone/sql.go core/phone/json.go core/phone/sql_test.go core/phone/json_test.go
git commit -m "feat(core/phone): SQL and JSON marshaling"
```

---

## Task 9: `phone` doc, benchmarks, optimization pass, roadmap

**Files:**
- Create: `core/phone/doc.go`, `core/phone/bench_test.go`
- Modify: `docs/packages.md` (delete the `core/phone` entry)

**Interfaces:**
- Consumes: the full `phone` API (Tasks 5–8).
- Produces: no new symbols.

- [ ] **Step 1: Write benchmarks**

`core/phone/bench_test.go`:
```go
package phone_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/country"
	"github.com/dmitrymomot/forge/core/phone"
)

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		_, _ = phone.Parse("+1 (415) 555-2671")
	}
}

func BenchmarkParseRegion(b *testing.B) {
	for b.Loop() {
		_, _ = phone.ParseRegion("07911 123456", "GB")
	}
}

func BenchmarkParserGate(b *testing.B) {
	set := country.NewSet(country.US, country.GB, country.DE)
	p, _ := phone.New(phone.WithAllowedCountries(set))
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Parse("+44 20 7946 0018")
	}
}
```

- [ ] **Step 2: Run the benchmarks (record before numbers)**

Run: `just bench ./core/phone/...`
Expected: results print; note ns/op and allocs/op. `Parse` allocates at least once (the stripped digit string); that is the candidate for the optimization pass.

- [ ] **Step 3: Write `doc.go`**

`core/phone/doc.go`:
```go
// Package phone normalizes phone numbers to E.164 and decomposes them against
// core/country's dial-code table. It parses messy human input into a canonical
// Phone value, formats it back out, and gates parsing by a supported-countries
// policy — deliberately without the libphonenumber machinery.
//
// Parse accepts a number that carries its own country code (a leading + or 00),
// stripping formatting characters. ParseRegion interprets bare national input
// using an alpha-2 region hint (stripping one leading trunk 0). A configured
// Parser (New with WithDefaultRegion and/or WithAllowedCountries) applies a
// default region to bare input and rejects numbers whose country is provably
// outside a country.Set with ErrUnsupportedRegion.
//
// A Phone exposes E164, DialCode, NationalNumber, Country, and Candidates.
// Because dial codes are shared (+1 covers the US, Canada, and the Caribbean),
// Country returns a stable primary with a false ok for an unresolved shared
// code — Candidates lists every possibility, and a region hint pins one. The
// gate mirrors this honesty: an ambiguous number passes when any candidate is
// supported, since it cannot be proven to belong to the unsupported one.
//
// Phone marshals to and from the E.164 string for SQL (Value/Scan, zero Phone
// is NULL) and JSON (zero Phone is null). What this is NOT: no per-country
// length/pattern validation, no line-type or carrier lookup, no pretty
// per-country grouping, and no area-code disambiguation of shared dial codes —
// the libphonenumber swamp stays out. Cheap E.164 shape validation lives in
// core/validate.
//
// # Usage
//
//	p, err := phone.Parse("+1 (415) 555-2671")
//	if err == nil {
//		_ = p.E164()           // "+14155552671"
//		_ = p.NationalNumber() // "4155552671"
//	}
//
//	set, _ := country.NewSetFromCodes("US", "GB", "DE")
//	parser, _ := phone.New(phone.WithDefaultRegion("US"), phone.WithAllowedCountries(set))
//	_, err = parser.Parse("+33 6 12 34 56 78") // ErrUnsupportedRegion
package phone
```

- [ ] **Step 4: Optimization pass (only if a benchmark showed a win)**

If `BenchmarkParse` shows an avoidable allocation (e.g., the separator-strip builder runs even when input is already clean), consider a fast path that skips the builder when no separators are present, and re-run `just bench ./core/phone/...`. Record before/after in the PR. Make no speculative changes; if the measured win is negligible, keep the simpler code and note "no change — measured win negligible".

- [ ] **Step 5: Delete the roadmap entry**

In `docs/packages.md`, remove the `**core/phone**` block (heading through its `Deps:` line and that entry's `---` separators). The `## core/` section header can remain if other entries exist; if `country` and `phone` were the only two, remove the now-empty `## core/` section.

- [ ] **Step 6: Verify and commit**

Run: `just lint && just test ./core/country/... && just test ./core/phone/...`
Expected: lint clean; all tests pass with `-race`.
```bash
git add core/phone/doc.go core/phone/bench_test.go docs/packages.md
git commit -m "docs(core/phone): doc.go, benchmarks, drop roadmap entry"
```

---

## Self-Review (completed)

**Spec coverage:**
- `country` type + all 7 fields → Task 1 (shape) + Task 3 (data). ✓
- ~249 exported vars → Task 1 (seed) + Task 3 (full). ✓
- Lookups `ByAlpha2/Alpha3/Numeric/ByDialCode/All` → Task 1. ✓
- `Set` (NewSet/NewSetFromCodes fail-closed/Contains/ContainsCode/All/Len) → Task 2. ✓
- Emoji derived at init → Task 1 (`flagEmoji` + init), guarded by Task 3 invariant test. ✓
- `Phone` type + `E164/DialCode/NationalNumber/IsZero` → Task 5. ✓
- `Parse` (+/00, length bounds, longest-prefix, error sentinels) → Task 5. ✓
- `ParseRegion` (trunk-0, region hint) + `Country`/`Candidates` (primary+false, resolved) → Task 6. ✓
- `Parser` `New`/`WithDefaultRegion`/`WithAllowedCountries` + provably-unsupported gate + fail-closed zero Set → Task 7. ✓
- SQL `Value`/`Scan`, JSON `Marshal`/`Unmarshal`, zero=NULL/null → Task 8. ✓
- Tenancy via passed value (no seam) → realized by `Set`/`Parser` being values; covered by Task 2 + Task 7 tests. ✓
- Perf: map-backed lookups, pointer-free `Phone`, benchmarks + optimization pass → Tasks 1/5 design + Tasks 4/9. ✓
- Black-box tests → every test file is `country_test`/`phone_test`. ✓
- Data provenance header → Task 1 `data.go` comment. ✓
- Build order country→phone, roadmap deletion → Tasks 4 and 9. ✓

**Placeholder scan:** No TBD/TODO; the one bulk-data step (Task 3) ships a full invariant test and an explicit canonical source, not a vague instruction. Optimization steps are conditioned on measured wins with an explicit "no change" fallback (not open-ended). ✓

**Type consistency:** `Country` fields, `Set` methods, `Phone` methods, and the four `phone` sentinels are used identically across tasks. `Parse`/`ParseRegion`/`build`/`toDigits`/`matchDial`/`primaryDial`/`gatePass` names match between definition and use. `New` returns `(*Parser, error)` consistently in Task 7 tests and impl. ✓
