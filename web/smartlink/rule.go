package smartlink

// ParamPolicy controls how Visit.Params merge into the final URL after macro
// rendering. ParamsFill and ParamsOverride preserve the target's original
// query pairs verbatim (order and encoding intact) and append merged params
// after them in sorted key order; an overridden key is rewritten in place.
type ParamPolicy int

const (
	// ParamsDrop never merges visit params into the URL (default).
	ParamsDrop ParamPolicy = iota
	// ParamsFill adds visit params only for keys the target URL doesn't set.
	ParamsFill
	// ParamsOverride replaces the target URL's same-key params with visit values.
	ParamsOverride
)

// Spec is the consumer-hydrated definition of one smartlink: ordered rules
// evaluated first-match-wins, with a mandatory default target. Rule values are
// consumer data (however stored) hydrated into this typed vocabulary.
type Spec struct {
	// Salt namespaces the Spec's sticky bucketing (weighted splits and Percent
	// matchers): two Specs with different salts bucket the same StickyKey
	// independently. Set it to a stable link identity — [Manager] uses the
	// link code for Target links, and [Cache] defaults an empty Salt to the
	// ref — so unrelated links never share split assignments. An empty Salt
	// on a hand-compiled Spec is legal but correlates its buckets with every
	// other unsalted Spec's.
	Salt string
	// Rules are evaluated in order; the first rule whose matchers all match
	// wins. May be empty for a default-only link.
	Rules []Rule
	// Default is the mandatory fallback target list; a weighted split when it
	// has more than one entry.
	Default []Target
	// Params is the merge policy for Visit.Params into the final URL.
	Params ParamPolicy
}

// Rule is one ordered decision row: a conjunction of matchers selecting a
// target list.
type Rule struct {
	// Name identifies the rule in Decision.Rule and, together with
	// [Spec.Salt], salts its Percent and split bucketing. Required and unique
	// within a Spec.
	Name string
	// When is the matcher conjunction (AND). An empty list matches every
	// visit — an unconditional override that shadows everything after it.
	When []Matcher
	// Targets holds one or more destinations; more than one forms a weighted
	// split bucketed by Visit.StickyKey.
	Targets []Target
}

// Target is one destination URL template with its split weight.
type Target struct {
	// URL is the destination template; it may contain {country}, {device},
	// {locale}, and {param.NAME} macros in the host, path, or query. Macro
	// values escape by position, so they can never alter the URL structure:
	// path and query values percent-encode, and a host-position value renders
	// only when it is entirely hostname-safe bytes ([A-Za-z0-9._~-]) — any
	// other value renders empty, yielding a dead but well-formed URL, never a
	// different destination shape.
	URL string
	// Weight is the target's relative share of a split; must be >= 1 when the
	// target list has more than one entry, ignored otherwise.
	Weight int
}
