package smartlink

import (
	"fmt"
	"math"
	"net/url"

	"github.com/dmitrymomot/forge/core/clock"
)

// Compiled is a compiled Spec ready for per-click decisions. Compile once per
// rule-set version and reuse; Decide is safe for concurrent use.
type Compiled struct {
	clock    clock.Clock
	rules    []compiledRule
	def      split
	params   ParamPolicy
	needsNow bool
}

// compiledRule is one validated rule with normalized matchers and a
// precomputed split.
type compiledRule struct {
	name    string
	when    []matcher
	targets split
}

// split is a target list with cumulative weights for deterministic
// sticky-key bucketing.
type split struct {
	targets []compiledTarget
	cum     []int // cumulative weights, len == len(targets); nil for a single target
	seed    uint64
}

// compiledTarget pairs the caller's raw target with its parsed URL template
// and, for a literal (macro-free) template, the pre-parsed URL decomposition
// param merges reuse instead of re-parsing an identical string per Decide.
type compiledTarget struct {
	lit  *litQuery
	raw  Target
	tmpl template
}

// litQuery is a literal target's URL parsed once at compile: the url.URL to
// stamp a merged query onto and the pre-split original query pairs.
type litQuery struct {
	u     url.URL
	pairs []qpair
}

// Compile validates a consumer-hydrated Spec fail-fast — missing default,
// bad rule or matcher values, malformed or unknown-macro templates are all
// construction errors — and returns the immutable decision engine for it.
func Compile(spec Spec, opts ...Option) (*Compiled, error) {
	cfg := newConfig(opts...)
	l := &Compiled{clock: cfg.clock, params: spec.Params}
	switch spec.Params {
	case ParamsDrop, ParamsFill, ParamsOverride:
	default:
		return nil, fmt.Errorf("%w: unknown ParamPolicy %d", ErrInvalidRule, spec.Params)
	}

	def, err := compileSplit(spec.Default, spec.Salt, "")
	if err != nil {
		return nil, err
	}
	l.def = def

	seen := make(map[string]struct{}, len(spec.Rules))
	l.rules = make([]compiledRule, 0, len(spec.Rules))
	for _, r := range spec.Rules {
		if r.Name == "" {
			return nil, fmt.Errorf("%w: empty rule name", ErrInvalidRule)
		}
		if _, dup := seen[r.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate rule name %q", ErrInvalidRule, r.Name)
		}
		seen[r.Name] = struct{}{}

		cr := compiledRule{name: r.Name, when: make([]matcher, 0, len(r.When))}
		for idx, m := range r.When {
			if m == nil {
				return nil, fmt.Errorf("%w: rule %q: nil matcher", ErrInvalidMatcher, r.Name)
			}
			cm, err := m.compile(spec.Salt, r.Name, idx)
			if err != nil {
				return nil, err
			}
			if cm.kind == matchTime {
				l.needsNow = true
			}
			cr.when = append(cr.when, cm)
		}
		cr.targets, err = compileSplit(r.Targets, spec.Salt, r.Name)
		if err != nil {
			return nil, err
		}
		l.rules = append(l.rules, cr)
	}
	return l, nil
}

// compileSplit validates a target list (rule targets or the default list,
// ruleName == "") and precomputes its templates and cumulative weights. salt
// namespaces the split's bucketing seed per Spec (see [Spec.Salt]) so two
// links with identical rule names bucket independently. Weight is only
// consulted for a multi-target list, per its contract.
func compileSplit(targets []Target, salt, ruleName string) (split, error) {
	where := "default targets"
	if ruleName != "" {
		where = fmt.Sprintf("rule %q", ruleName)
	}
	if len(targets) == 0 {
		if ruleName == "" {
			return split{}, ErrNoDefault
		}
		return split{}, fmt.Errorf("%w: %s: no targets", ErrInvalidRule, where)
	}
	s := split{targets: make([]compiledTarget, len(targets))}
	for i, t := range targets {
		if t.URL == "" {
			return split{}, fmt.Errorf("%w: %s: empty URL", ErrInvalidTarget, where)
		}
		if len(targets) > 1 && t.Weight < 1 {
			return split{}, fmt.Errorf("%w: %s: target %q weight %d", ErrInvalidTarget, where, t.URL, t.Weight)
		}
		tmpl, err := parseTemplate(t.URL, where)
		if err != nil {
			return split{}, err
		}
		s.targets[i] = compiledTarget{raw: t, tmpl: tmpl}
		if tmpl.segs == nil {
			if u, err := url.Parse(tmpl.raw); err == nil {
				s.targets[i].lit = &litQuery{u: *u, pairs: splitQuery(u.RawQuery)}
			}
		}
	}
	if len(targets) > 1 {
		s.seed = hashString(fnvOffset, "s\x00"+salt+"\x00"+ruleName)
		s.cum = make([]int, len(targets))
		var total int64
		for i, t := range targets {
			total += int64(t.Weight)
			if total > math.MaxInt32 {
				return split{}, fmt.Errorf("%w: %s: split weights sum past %d", ErrInvalidTarget, where, math.MaxInt32)
			}
			s.cum[i] = int(total)
		}
	}
	return s, nil
}
