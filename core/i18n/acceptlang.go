package i18n

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Bounds on Accept-Language, which is attacker-controlled: without them a
// client can make the server parse and sort megabytes of tags.
const (
	// maxAcceptLangLen caps the header size. Real headers are well under 200
	// bytes; 4 KB is generous.
	maxAcceptLangLen = 4096
	// maxAcceptLangTags caps how many tags are parsed. Beyond it the header is
	// truncated in header order — not rejected, and not sorted by q first: the
	// cap has to run before the sort, or an attacker could force the sort to
	// touch every tag it is meant to bound. Clients list ranges
	// most-preferred-first, so in practice the kept prefix is the client's top
	// preferences; only a client that both sends over 32 ranges and orders them
	// against their own q values would drop a preference here.
	maxAcceptLangTags = 32
)

// langQ is one parsed Accept-Language entry.
type langQ struct {
	tag string
	q   float64
}

// parseAcceptLanguage parses and sorts an Accept-Language header, highest
// quality first. Entries with an invalid, out-of-range, NaN, or zero q are
// dropped. The sort is stable, so equal-q tags keep header order — the
// caller's first supported match among them is the server-preference
// tie-break (RFC 7231 5.3.1).
func parseAcceptLanguage(header string) []langQ {
	if header == "" || len(header) > maxAcceptLangLen {
		return nil
	}
	out := make([]langQ, 0, 8)
	for part := range strings.SplitSeq(header, ",") {
		if len(out) >= maxAcceptLangTags {
			break
		}
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, params, _ := strings.Cut(part, ";")
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		q, ok := parseQ(params)
		if !ok || q <= 0 {
			continue // unparseable, out of range, NaN, or "not acceptable"
		}
		if tag != "*" {
			if tag = normalizeTag(tag); tag == "" {
				continue
			}
		}
		out = append(out, langQ{tag: tag, q: q})
	}
	// slices.SortStableFunc over sort.SliceStable: sort.Slice* builds a
	// reflection-based swapper per call, which profiles show costing ~1/3
	// of Negotiate's allocations for a header this small.
	slices.SortStableFunc(out, func(a, b langQ) int { return cmp.Compare(b.q, a.q) })
	return out
}

// parseQ reads the q parameter from a language-range's parameter string. An
// absent parameter means q=1. ok is false for anything unparseable, out of
// [0,1], NaN, or a parameter that is not q. NaN especially, because it
// compares false against everything and would corrupt the sort.
func parseQ(params string) (float64, bool) {
	params = strings.TrimSpace(params)
	if params == "" {
		return 1, true // "en" with no parameters
	}
	key, val, ok := strings.Cut(params, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
		return 0, false // not a q parameter at all
	}
	q, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
	if err != nil || math.IsNaN(q) || q < 0 || q > 1 {
		return 0, false
	}
	return q, true
}

// negotiate picks the best supported locale for header. ok is false when
// nothing matches, which lets a caller (e.g. the middleware's resolver chain)
// fall through to another source before defaulting.
func (b *Bundle) negotiate(header string) (Locale, bool) {
	for _, lq := range parseAcceptLanguage(header) {
		if lq.tag == "*" {
			return b.Default(), true
		}
		// Parse applies exact-then-base matching, so negotiation and lookup
		// agree by construction.
		if loc, err := b.Parse(lq.tag); err == nil {
			return loc, true
		}
	}
	return Locale{}, false
}

// Negotiate picks the best supported locale for an Accept-Language header,
// falling back to the bundle default when the header is empty, malformed, or
// names nothing the bundle supports. Matching runs against the locales the
// application's catalogs declare — there is no framework list to miss.
func (b *Bundle) Negotiate(header string) Locale {
	if loc, ok := b.negotiate(header); ok {
		return loc
	}
	return b.Default()
}
