package formula

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dmitrymomot/forge/core/decimal"
)

// Result is the explanation record of one evaluation: the spec identity, every
// input provided (used or not, sorted by name), and every stage with each
// term's contribution. It is the statement line item and the dispute answer —
// persist it next to what it justifies. Serialization is deterministic:
// evaluating the same Compiled spec with the same inputs (same values at the
// same scales) yields a Result whose json.Marshal output byte-matches.
type Result struct {
	SpecVersion     string        `json:"spec_version"`
	SpecFingerprint string        `json:"spec_fingerprint"`
	Inputs          []InputValue  `json:"inputs"`
	Stages          []StageResult `json:"stages"`
}

// InputValue is one named decimal value, used for the input snapshot and for
// func-stage arg records.
type InputValue struct {
	Name  string          `json:"name"`
	Value decimal.Decimal `json:"value"`
}

// StageResult explains one stage: the exact pre-round pre-clamp sum (Raw), the
// final value after round and clamp (Value), and either the per-term
// breakdown (linear stages) or the func name with its resolved args (func
// stages).
type StageResult struct {
	Name string `json:"name"`
	// Terms breaks a linear stage down: Raw is the exact sum of their
	// contributions.
	Terms []TermResult `json:"terms,omitempty"`
	// Func and Args record a func stage's function name and the arg values it
	// received.
	Func string       `json:"func,omitempty"`
	Args []InputValue `json:"args,omitempty"`
	// Raw is the stage value before rounding and clamping.
	Raw decimal.Decimal `json:"raw"`
	// Value is the final stage value — what later stages and the caller see.
	Value decimal.Decimal `json:"value"`
	// Clamped reports whether a clamp bound replaced the rounded value.
	Clamped bool `json:"clamped,omitempty"`
}

// TermResult explains one linear term: Contribution = Coefficient ×
// MetricValue, exact.
type TermResult struct {
	Metric       string          `json:"metric"`
	Coefficient  decimal.Decimal `json:"coefficient"`
	MetricValue  decimal.Decimal `json:"metric_value"`
	Contribution decimal.Decimal `json:"contribution"`
}

// Value returns the final value of the named stage and whether it exists.
// Inputs are not looked up — only derived metrics.
func (r Result) Value(name string) (decimal.Decimal, bool) {
	for _, st := range r.Stages {
		if st.Name == name {
			return st.Value, true
		}
	}
	return decimal.Decimal{}, false
}

// Final returns the last stage's value — by convention the metric the spec
// exists to derive. It returns zero for an empty Result.
func (r Result) Final() decimal.Decimal {
	if len(r.Stages) == 0 {
		return decimal.Decimal{}
	}
	return r.Stages[len(r.Stages)-1].Value
}

// Eval runs the spec over inputs and returns the full explanation. Stages are
// evaluated in order; each consumes prior stages' final (post-round,
// post-clamp) values. Extra inputs are allowed and recorded; a missing one
// fails with ErrUnknownMetric, an input named like a stage fails with
// ErrMetricCollision, and a func error fails with ErrFuncFailed wrapping the
// cause. Evaluation is deterministic: no clocks, no randomness, exact decimal
// arithmetic with rounding only where the spec says so.
func (c *Compiled) Eval(inputs map[string]decimal.Decimal) (Result, error) {
	for name := range inputs {
		if _, clash := c.stageIndex[name]; clash {
			return Result{}, fmt.Errorf("%w: %q", ErrMetricCollision, name)
		}
	}

	values := make([]decimal.Decimal, len(c.stages))
	stages := make([]StageResult, len(c.stages))
	for i, st := range c.stages {
		sr := StageResult{Name: st.name}
		var raw decimal.Decimal
		if st.fn != nil {
			args := make([]decimal.Decimal, len(st.args))
			sr.Func = st.fnName
			if len(st.args) > 0 {
				sr.Args = make([]InputValue, len(st.args))
			}
			for j, r := range st.args {
				v, err := resolveValue(r, values, inputs)
				if err != nil {
					return Result{}, fmt.Errorf("formula: stage %q: %w", st.name, err)
				}
				args[j] = v
				sr.Args[j] = InputValue{Name: st.argNames[j], Value: v}
			}
			v, err := st.fn(args)
			if err != nil {
				return Result{}, fmt.Errorf("%w: stage %q func %q: %w", ErrFuncFailed, st.name, st.fnName, err)
			}
			raw = v
		} else {
			sr.Terms = make([]TermResult, len(st.terms))
			for j, t := range st.terms {
				v, err := resolveValue(t.ref, values, inputs)
				if err != nil {
					return Result{}, fmt.Errorf("formula: stage %q: %w", st.name, err)
				}
				contribution := t.coefficient.Mul(v)
				raw = raw.Add(contribution)
				sr.Terms[j] = TermResult{
					Metric:       termMetric(t.ref, c),
					Coefficient:  t.coefficient,
					MetricValue:  v,
					Contribution: contribution,
				}
			}
		}
		sr.Raw = raw

		value := raw
		if st.round != nil {
			value = value.Round(st.round.Scale, st.round.Mode)
		}
		if st.clamp != nil {
			if st.clamp.Min != nil && value.Cmp(*st.clamp.Min) < 0 {
				value = *st.clamp.Min
				sr.Clamped = true
			}
			if st.clamp.Max != nil && value.Cmp(*st.clamp.Max) > 0 {
				value = *st.clamp.Max
				sr.Clamped = true
			}
		}
		sr.Value = value
		values[i] = value
		stages[i] = sr
	}

	snapshot := make([]InputValue, 0, len(inputs))
	for name, value := range inputs {
		snapshot = append(snapshot, InputValue{Name: name, Value: value})
	}
	slices.SortFunc(snapshot, func(a, b InputValue) int { return strings.Compare(a.Name, b.Name) })

	return Result{
		SpecVersion:     c.spec.Version,
		SpecFingerprint: c.fingerprint,
		Inputs:          snapshot,
		Stages:          stages,
	}, nil
}

func resolveValue(r ref, values []decimal.Decimal, inputs map[string]decimal.Decimal) (decimal.Decimal, error) {
	if r.stage >= 0 {
		return values[r.stage], nil
	}
	v, ok := inputs[r.input]
	if !ok {
		return decimal.Decimal{}, fmt.Errorf("%w: %q", ErrUnknownMetric, r.input)
	}
	return v, nil
}

func termMetric(r ref, c *Compiled) string {
	if r.stage >= 0 {
		return c.stages[r.stage].name
	}
	return r.input
}
