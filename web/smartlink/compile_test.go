package smartlink_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/smartlink"
)

func defTargets() []smartlink.Target {
	return []smartlink.Target{{URL: "https://example.com/"}}
}

func TestCompileValidation(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		spec smartlink.Spec
		want error
	}{
		{"empty spec", smartlink.Spec{}, smartlink.ErrNoDefault},
		{"no default with rules", smartlink.Spec{Rules: []smartlink.Rule{{Name: "r", Targets: defTargets()}}}, smartlink.ErrNoDefault},
		{"default empty URL", smartlink.Spec{Default: []smartlink.Target{{}}}, smartlink.ErrInvalidTarget},
		{"default split zero weight", smartlink.Spec{Default: []smartlink.Target{{URL: "https://a.com", Weight: 1}, {URL: "https://b.com"}}}, smartlink.ErrInvalidTarget},
		{"negative weight single target", smartlink.Spec{Default: []smartlink.Target{{URL: "https://a.com", Weight: -1}}}, smartlink.ErrInvalidTarget},
		{"split weight overflow", smartlink.Spec{Default: []smartlink.Target{
			{URL: "https://a.com", Weight: math.MaxInt32}, {URL: "https://b.com", Weight: 1},
		}}, smartlink.ErrInvalidTarget},
		{"bad param policy", smartlink.Spec{Default: defTargets(), Params: smartlink.ParamPolicy(99)}, smartlink.ErrInvalidRule},
		{"empty rule name", smartlink.Spec{Default: defTargets(), Rules: []smartlink.Rule{{Targets: defTargets()}}}, smartlink.ErrInvalidRule},
		{"duplicate rule name", smartlink.Spec{Default: defTargets(), Rules: []smartlink.Rule{
			{Name: "r", Targets: defTargets()}, {Name: "r", Targets: defTargets()},
		}}, smartlink.ErrInvalidRule},
		{"rule without targets", smartlink.Spec{Default: defTargets(), Rules: []smartlink.Rule{{Name: "r"}}}, smartlink.ErrInvalidRule},
		{"nil matcher", smartlink.Spec{Default: defTargets(), Rules: []smartlink.Rule{
			{Name: "r", When: []smartlink.Matcher{nil}, Targets: defTargets()},
		}}, smartlink.ErrInvalidMatcher},
		{"geo empty", ruleSpec(smartlink.Geo{}), smartlink.ErrInvalidMatcher},
		{"geo three letters", ruleSpec(smartlink.Geo{Countries: []string{"USA"}}), smartlink.ErrInvalidMatcher},
		{"geo digit", ruleSpec(smartlink.Geo{Countries: []string{"D1"}}), smartlink.ErrInvalidMatcher},
		{"device empty list", ruleSpec(smartlink.Device{}), smartlink.ErrInvalidMatcher},
		{"device empty value", ruleSpec(smartlink.Device{Devices: []string{""}}), smartlink.ErrInvalidMatcher},
		{"locale empty list", ruleSpec(smartlink.Locale{}), smartlink.ErrInvalidMatcher},
		{"param equals no key", ruleSpec(smartlink.ParamEquals{Values: []string{"x"}}), smartlink.ErrInvalidMatcher},
		{"param equals no values", ruleSpec(smartlink.ParamEquals{Key: "k"}), smartlink.ErrInvalidMatcher},
		{"time window unbounded", ruleSpec(smartlink.TimeWindow{}), smartlink.ErrInvalidMatcher},
		{"time window inverted", ruleSpec(smartlink.TimeWindow{From: at, Until: at.Add(-time.Hour)}), smartlink.ErrInvalidMatcher},
		{"time window empty", ruleSpec(smartlink.TimeWindow{From: at, Until: at}), smartlink.ErrInvalidMatcher},
		{"percent zero", ruleSpec(smartlink.Percent{}), smartlink.ErrInvalidMatcher},
		{"percent hundred", ruleSpec(smartlink.Percent{Share: 100}), smartlink.ErrInvalidMatcher},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := smartlink.Compile(tc.spec); !errors.Is(err, tc.want) {
				t.Fatalf("Compile() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCompileTemplateValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
		want error
	}{
		{"unclosed brace", "https://a.com/{country", smartlink.ErrInvalidTemplate},
		{"unmatched close", "https://a.com/}x", smartlink.ErrInvalidTemplate},
		{"close before open", "https://a.com/}{country}", smartlink.ErrInvalidTemplate},
		{"trailing close after macro", "https://a.com/{country}}", smartlink.ErrInvalidTemplate},
		{"bad url escape", "https://a.com/%zz", smartlink.ErrInvalidTemplate},
		{"unknown macro", "https://a.com/{ip}", smartlink.ErrUnknownMacro},
		{"empty param macro", "https://a.com/{param.}", smartlink.ErrUnknownMacro},
		{"empty macro", "https://a.com/{}", smartlink.ErrUnknownMacro},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := smartlink.Spec{Default: []smartlink.Target{{URL: tc.url}}}
			if _, err := smartlink.Compile(spec); !errors.Is(err, tc.want) {
				t.Fatalf("Compile() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// ruleSpec wraps a single matcher into an otherwise-valid Spec.
func ruleSpec(m smartlink.Matcher) smartlink.Spec {
	return smartlink.Spec{
		Default: defTargets(),
		Rules:   []smartlink.Rule{{Name: "r", When: []smartlink.Matcher{m}, Targets: defTargets()}},
	}
}
