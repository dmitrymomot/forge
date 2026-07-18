package smartlink

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Matcher is one typed condition in a rule's conjunction. The set is closed —
// exactly [Geo], [Device], [Locale], [ParamEquals], [TimeWindow], and
// [Percent] — rule values are consumer data hydrated into these types, never a
// DSL.
type Matcher interface {
	// compile validates and normalizes the matcher into its evaluated form,
	// sealing the interface to this package. salt is the Spec's bucketing salt
	// and idx the matcher's position within the rule's conjunction — both feed
	// Percent's seed so distinct links and distinct Percent gates in one rule
	// bucket independently.
	compile(salt, ruleName string, idx int) (matcher, error)
}

// matcherKind discriminates the compiled matcher union.
type matcherKind uint8

const (
	matchGeo matcherKind = iota
	matchDevice
	matchLocale
	matchParam
	matchTime
	matchPercent
)

// matcher is the compiled, normalized form evaluated on every Decide — a
// tagged union rather than an interface so the hot path stays devirtualized
// and the Visit never escapes (measured: 2 allocs → 0 on the literal decide;
// see bench_test.go).
type matcher struct {
	from   time.Time
	until  time.Time
	key    string   // matchParam: the visit param key
	values []string // matchGeo/matchDevice/matchLocale/matchParam: normalized any-of values
	seed   uint64   // matchPercent: precomputed FNV state over the rule-name salt
	share  uint64   // matchPercent: 1..99
	kind   matcherKind
}

// match reports whether the visit satisfies this condition. now is only
// meaningful for matchTime (resolved once per Decide when a link has any
// TimeWindow).
func (m *matcher) match(v *Visit, now time.Time) bool {
	switch m.kind {
	case matchGeo:
		return containsFold(m.values, v.Country)
	case matchDevice:
		return containsFold(m.values, v.Device)
	case matchLocale:
		return localeMatches(m.values, v.Locale)
	case matchParam:
		pv, ok := v.Params[m.key]
		return ok && slices.Contains(m.values, pv)
	case matchTime:
		if !m.from.IsZero() && now.Before(m.from) {
			return false
		}
		return m.until.IsZero() || now.Before(m.until)
	case matchPercent:
		if v.StickyKey == "" {
			return false
		}
		return hashString(m.seed, v.StickyKey)%100 < m.share
	}
	return false // unreachable: compile only emits the kinds above
}

// Geo matches when the visit country equals any listed ISO 3166-1 alpha-2
// code (any case).
type Geo struct {
	Countries []string
}

func (m Geo) compile(_, ruleName string, _ int) (matcher, error) {
	if len(m.Countries) == 0 {
		return matcher{}, fmt.Errorf("%w: rule %q: Geo needs at least one country", ErrInvalidMatcher, ruleName)
	}
	countries := make([]string, len(m.Countries))
	for i, c := range m.Countries {
		if len(c) != 2 || !isAlpha(c) {
			return matcher{}, fmt.Errorf("%w: rule %q: Geo country %q is not an alpha-2 code", ErrInvalidMatcher, ruleName, c)
		}
		countries[i] = strings.ToUpper(c)
	}
	return matcher{kind: matchGeo, values: countries}, nil
}

// Device matches when the visit device class equals any listed value
// (case-insensitive; web/useragent DeviceType vocabulary by convention).
type Device struct {
	Devices []string
}

func (m Device) compile(_, ruleName string, _ int) (matcher, error) {
	devices, err := nonEmptyLower(m.Devices, ruleName, "Device")
	if err != nil {
		return matcher{}, err
	}
	return matcher{kind: matchDevice, values: devices}, nil
}

// Locale matches the visit locale against any listed BCP-47-style tag,
// case-insensitively. A bare-language value ("en") also matches any
// region-qualified visit locale with that primary subtag ("en-US"); a
// region-qualified value requires a full match.
type Locale struct {
	Locales []string
}

func (m Locale) compile(_, ruleName string, _ int) (matcher, error) {
	locales, err := nonEmptyLower(m.Locales, ruleName, "Locale")
	if err != nil {
		return matcher{}, err
	}
	return matcher{kind: matchLocale, values: locales}, nil
}

// localeMatches implements Locale semantics against pre-lowercased rule values.
func localeMatches(locales []string, visit string) bool {
	primary, _, _ := strings.Cut(visit, "-")
	for _, l := range locales {
		if strings.EqualFold(visit, l) {
			return true
		}
		if !strings.Contains(l, "-") && strings.EqualFold(primary, l) {
			return true
		}
	}
	return false
}

// ParamEquals matches when the visit param Key equals any listed value
// (exact, case-sensitive — sub-IDs are case-sensitive).
type ParamEquals struct {
	Key    string
	Values []string
}

func (m ParamEquals) compile(_, ruleName string, _ int) (matcher, error) {
	if m.Key == "" {
		return matcher{}, fmt.Errorf("%w: rule %q: ParamEquals needs a key", ErrInvalidMatcher, ruleName)
	}
	if len(m.Values) == 0 {
		return matcher{}, fmt.Errorf("%w: rule %q: ParamEquals %q needs at least one value", ErrInvalidMatcher, ruleName, m.Key)
	}
	return matcher{kind: matchParam, key: m.Key, values: slices.Clone(m.Values)}, nil
}

// TimeWindow matches when the decision time t satisfies From <= t < Until as
// absolute instants. Either bound may be zero (open-ended); both zero, or
// Until not after From, is a compile error. t is Visit.At when set, otherwise
// the link's clock.
type TimeWindow struct {
	From  time.Time
	Until time.Time
}

func (m TimeWindow) compile(_, ruleName string, _ int) (matcher, error) {
	if m.From.IsZero() && m.Until.IsZero() {
		return matcher{}, fmt.Errorf("%w: rule %q: TimeWindow needs at least one bound", ErrInvalidMatcher, ruleName)
	}
	if !m.From.IsZero() && !m.Until.IsZero() && !m.Until.After(m.From) {
		return matcher{}, fmt.Errorf("%w: rule %q: TimeWindow Until must be after From", ErrInvalidMatcher, ruleName)
	}
	return matcher{kind: matchTime, from: m.From, until: m.Until}, nil
}

// Percent matches a deterministic Share percent of traffic, bucketed by hash
// of the Spec salt, rule name, the matcher's position in the rule, and
// Visit.StickyKey — the same visitor always lands on the same side, distinct
// links bucket independently (see [Spec.Salt]), and two Percent gates in one
// rule compose as independent draws instead of collapsing into the wider one.
// Reordering a rule's matchers therefore reassigns its Percent buckets.
// Share must be 1..99 (0 is a dead rule, 100 is no gate — both compile
// errors). An empty StickyKey never matches (fails closed past this rule).
type Percent struct {
	Share int
}

func (m Percent) compile(salt, ruleName string, idx int) (matcher, error) {
	if m.Share < 1 || m.Share > 99 {
		return matcher{}, fmt.Errorf("%w: rule %q: Percent share %d outside 1..99", ErrInvalidMatcher, ruleName, m.Share)
	}
	seed := hashString(fnvOffset, "p\x00"+salt+"\x00"+ruleName+"\x00"+strconv.Itoa(idx))
	return matcher{kind: matchPercent, seed: seed, share: uint64(m.Share)}, nil
}

// isAlpha reports whether s is ASCII letters only.
func isAlpha(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// nonEmptyLower validates a non-empty list of non-empty values and returns a
// lowercased copy.
func nonEmptyLower(values []string, ruleName, kind string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: rule %q: %s needs at least one value", ErrInvalidMatcher, ruleName, kind)
	}
	out := make([]string, len(values))
	for i, v := range values {
		if v == "" {
			return nil, fmt.Errorf("%w: rule %q: %s has an empty value", ErrInvalidMatcher, ruleName, kind)
		}
		out[i] = strings.ToLower(v)
	}
	return out, nil
}

// containsFold reports whether list (pre-normalized at compile) contains s
// case-insensitively. No allocations.
func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

// hashString continues an FNV-1a 64 hash over s from state h, with a trailing
// separator multiply so concatenated segments can't collide with shifted
// boundaries. Inlined to stay allocation-free on the hot path (featureflag
// precedent).
func hashString(h uint64, s string) uint64 {
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= fnvPrime
	}
	h *= fnvPrime // NUL separator: h ^= 0 is a no-op, the multiply still mixes
	return h
}
