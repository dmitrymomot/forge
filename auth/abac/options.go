package abac

type config struct {
	rules []Rule
}

// Option configures New.
type Option func(*config)

// WithRules adds rules to the policy. Accumulates across calls, so rule sets
// can be assembled from multiple call sites (feature modules registering their
// own predicates).
func WithRules(rules ...Rule) Option {
	return func(c *config) { c.rules = append(c.rules, rules...) }
}
