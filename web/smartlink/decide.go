package smartlink

import (
	"net/url"
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
// split target by sticky-key bucket, and renders the final URL. It never
// fails: every failure mode is a Compile error.
func (l *Compiled) Decide(v Visit) Decision {
	var now time.Time
	if l.needsNow {
		now = v.At
		if now.IsZero() {
			now = l.clock.Now()
		}
	}
	for i := range l.rules {
		r := &l.rules[i]
		if matchAll(r.when, &v, now) {
			t := r.targets.pick(v.StickyKey)
			return Decision{Rule: r.name, Target: t.raw, URL: l.finalURL(t, &v)}
		}
	}
	t := l.def.pick(v.StickyKey)
	return Decision{Target: t.raw, URL: l.finalURL(t, &v)}
}

// matchAll reports whether every matcher in the conjunction matches; an empty
// conjunction always matches.
func matchAll(when []matcher, v *Visit, now time.Time) bool {
	for i := range when {
		if !when[i].match(v, now) {
			return false
		}
	}
	return true
}

// pick buckets the sticky key into the split's cumulative weights. A single
// target returns directly; an empty key deterministically takes the first
// target.
func (s *split) pick(stickyKey string) *compiledTarget {
	if s.cum == nil || stickyKey == "" {
		return &s.targets[0]
	}
	n := int(hashString(s.seed, stickyKey) % s.total)
	for i, c := range s.cum {
		if n < c {
			return &s.targets[i]
		}
	}
	return &s.targets[len(s.targets)-1] // unreachable: n < total == cum[last]
}

// finalURL renders the target's macros and merges visit params per the link
// policy.
func (l *Compiled) finalURL(t *compiledTarget, v *Visit) string {
	rendered := t.tmpl.render(v)
	if l.params == ParamsDrop || len(v.Params) == 0 {
		return rendered
	}
	u, err := url.Parse(rendered)
	if err != nil {
		// Unreachable: the template parsed at compile and macro values are
		// escaped, so the rendered URL preserves the validated structure.
		return rendered
	}
	q := u.Query()
	for k, val := range v.Params {
		if k == "" {
			continue
		}
		if l.params == ParamsOverride || !q.Has(k) {
			q.Set(k, val)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
