package bizcal

import (
	"errors"
	"testing"
	"time"
)

// validateRule is unexported and exercised here directly; New (Task 3) is
// its real caller, but pinning its behavior against every rule kind now
// avoids regressions once that wiring lands.
func TestValidateRule(t *testing.T) {
	cases := []struct {
		name    string
		rule    Rule
		wantErr bool
	}{
		{"fixed ordinary", Fixed{Month: time.January, Day: 1}, false},
		{"fixed feb 29 is valid", Fixed{Month: time.February, Day: 29}, false},
		{"fixed feb 30 is invalid", Fixed{Month: time.February, Day: 30}, true},
		{"fixed month out of range", Fixed{Month: time.Month(13), Day: 1}, true},
		{"nth weekday ordinary", NthWeekday{Month: time.November, Weekday: time.Thursday, N: 4}, false},
		{"nth weekday from end", NthWeekday{Month: time.May, Weekday: time.Monday, N: -1}, false},
		{"nth weekday n=0 is invalid", NthWeekday{Month: time.May, Weekday: time.Monday, N: 0}, true},
		{"nth weekday n=6 is invalid", NthWeekday{Month: time.May, Weekday: time.Monday, N: 6}, true},
		{"nth weekday n=-6 is invalid", NthWeekday{Month: time.May, Weekday: time.Monday, N: -6}, true},
		{"nth weekday n=5 is valid", NthWeekday{Month: time.May, Weekday: time.Monday, N: 5}, false},
		{"nth weekday n=-5 is valid", NthWeekday{Month: time.May, Weekday: time.Monday, N: -5}, false},
		{"rulefunc always valid", RuleFunc(func(year int) []Date { return nil }), false},
		{"observed delegates to inner valid", Observed(Fixed{Month: time.July, Day: 4}), false},
		{"observed delegates to inner invalid", Observed(Fixed{Month: time.February, Day: 30}), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRule(c.rule)
			if c.wantErr && !errors.Is(err, ErrInvalidRule) {
				t.Fatalf("validateRule(%+v) = %v, want ErrInvalidRule", c.rule, err)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("validateRule(%+v) = %v, want nil", c.rule, err)
			}
		})
	}
}
