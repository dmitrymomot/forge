package smartlink

// ParamPolicy controls how Visit.Params merge into the final URL after macro
// rendering.
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
	// Name identifies the rule in Decision.Rule and salts its Percent and
	// split bucketing. Required and unique within a Spec.
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
	// {locale}, and {param.NAME} macros.
	URL string
	// Weight is the target's relative share of a split; must be >= 1 when the
	// target list has more than one entry, ignored otherwise.
	Weight int
}
