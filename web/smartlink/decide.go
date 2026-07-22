package smartlink

import (
	"net/url"
	"slices"
	"strings"
	"time"
)

// Decision is the outcome of one Decide call — what the caller redirects to
// and emits as the click event.
type Decision struct {
	// Rule is the matched rule's name; empty when the default target was used.
	Rule string
	// URL is the final destination: macros rendered, visit params merged per
	// the link's ParamPolicy.
	URL string
	// Target is the chosen raw (template-form) target, before rendering.
	Target Target
}

// Decide evaluates the visit against the link's rules in order — first rule
// whose matchers all match wins, falling back to the default target — picks a
// split target by sticky-key bucket, and renders the final URL.
//
// Decide fails closed on missing facts: when the outcome would depend on a
// visit field the visit doesn't carry — a Geo rule with no Country (geoip
// miss), a Device or Locale rule with the fact empty, a Percent gate or
// weighted split with no StickyKey — it returns [ErrMissingFact] instead of
// silently falling through to the default target. The gate is exact, not
// eager: a missing fact never errors when the decision is provably
// independent of it — the rule consulting it is already definitively false
// on another matcher, or an earlier rule matched outright. A spec whose
// consulted facts are all present — in particular the degenerate
// single-default-target link — never fails.
func (l *Compiled) Decide(v Visit) (Decision, error) {
	var now time.Time
	if l.needsNow {
		now = v.At
		if now.IsZero() {
			now = l.clock.Now()
		}
	}
	for i := range l.rules {
		r := &l.rules[i]
		ok, err := matchAll(r.when, &v, now)
		if err != nil {
			return Decision{}, err
		}
		if !ok {
			continue
		}
		t, err := r.targets.pick(v.StickyKey)
		if err != nil {
			return Decision{}, err
		}
		return Decision{Rule: r.name, Target: t.raw, URL: l.finalURL(t, &v)}, nil
	}
	t, err := l.def.pick(v.StickyKey)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Target: t.raw, URL: l.finalURL(t, &v)}, nil
}

// matchAll reports whether every matcher in the conjunction matches; an empty
// conjunction always matches. Evaluation is three-valued: a matcher whose
// visit fact is missing is unknowable, so the whole conjunction is scanned —
// any definitively-false matcher settles the rule as non-matching regardless
// of order, and only when the rule would otherwise match does the first
// missing fact's precomputed error surface.
func matchAll(when []matcher, v *Visit, now time.Time) (bool, error) {
	var miss error
	for i := range when {
		switch when[i].eval(v, now) {
		case evalFalse:
			return false, nil
		case evalMissing:
			if miss == nil {
				miss = when[i].missErr
			}
		case evalTrue:
		}
	}
	return miss == nil, miss
}

// pick buckets the sticky key into the split's cumulative weights. A single
// target returns directly; a weighted split with an empty key fails closed
// with the split's precomputed [ErrMissingFact] — a visit with no bucketing
// identity must not silently collapse onto the first target.
func (s *split) pick(stickyKey string) (*compiledTarget, error) {
	if s.cum == nil {
		return &s.targets[0], nil
	}
	if stickyKey == "" {
		return nil, s.missErr
	}
	n := int(hashString(s.seed, stickyKey) % uint64(s.cum[len(s.cum)-1]))
	for i, c := range s.cum {
		if n < c {
			return &s.targets[i], nil
		}
	}
	return &s.targets[len(s.targets)-1], nil // unreachable: n < cum[last]
}

// finalURL renders the target's macros and merges visit params per the link
// policy. Original query pairs are preserved verbatim (order and encoding
// intact — a pair url.Values could not round-trip, like an unencoded '%' or a
// ';', is never silently dropped); merged params append in sorted key order.
// A literal target reuses its compile-time parse instead of re-parsing the
// identical string per decide.
func (l *Compiled) finalURL(t *compiledTarget, v *Visit) string {
	if l.params == ParamsDrop || len(v.Params) == 0 {
		return t.tmpl.render(v)
	}
	override := l.params == ParamsOverride
	if t.lit != nil {
		u := t.lit.u
		u.RawQuery = mergeQuery(t.lit.pairs, v.Params, override)
		return u.String()
	}
	rendered := t.tmpl.render(v)
	u, err := url.Parse(rendered)
	if err != nil {
		// Unreachable: the template parsed at compile and macro values are
		// escaped, so the rendered URL preserves the validated structure.
		return rendered
	}
	u.RawQuery = mergeQuery(splitQuery(u.RawQuery), v.Params, override)
	return u.String()
}

// qpair is one raw query pair with its best-effort decoded key.
type qpair struct {
	raw string // the pair verbatim as it appeared in the query
	key string // decoded key; "" when the key does not decode (opaque pair)
}

// splitQuery splits rawQuery on '&', keeping each non-empty pair verbatim.
// Keys are decoded best-effort for merge comparisons only; an undecodable key
// leaves the pair opaque — always preserved, never matched by a param.
func splitQuery(rawQuery string) []qpair {
	if rawQuery == "" {
		return nil
	}
	pairs := make([]qpair, 0, strings.Count(rawQuery, "&")+1)
	for p := range strings.SplitSeq(rawQuery, "&") {
		if p == "" {
			continue
		}
		rawKey, _, _ := strings.Cut(p, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil {
			key = ""
		}
		pairs = append(pairs, qpair{raw: p, key: key})
	}
	return pairs
}

// mergeQuery merges params into pairs per policy: fill keeps every original
// pair and appends only params whose key the target does not already set;
// override rewrites the first occurrence of a matched key in place (dropping
// its duplicates, like url.Values.Set) and appends the rest. Appended params
// are sorted by key for deterministic output.
func mergeQuery(pairs []qpair, params map[string]string, override bool) string {
	var b strings.Builder
	matched := make(map[string]struct{}, len(params))
	for _, p := range pairs {
		if v, ok := params[p.key]; ok && p.key != "" {
			if override {
				if _, done := matched[p.key]; done {
					continue
				}
				matched[p.key] = struct{}{}
				writePair(&b, url.QueryEscape(p.key)+"="+url.QueryEscape(v))
				continue
			}
			matched[p.key] = struct{}{}
		}
		writePair(&b, p.raw)
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "" {
			continue
		}
		if _, ok := matched[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		writePair(&b, url.QueryEscape(k)+"="+url.QueryEscape(params[k]))
	}
	return b.String()
}

// writePair appends pair to b with the '&' separator when b is non-empty.
func writePair(b *strings.Builder, pair string) {
	if b.Len() > 0 {
		b.WriteByte('&')
	}
	b.WriteString(pair)
}
