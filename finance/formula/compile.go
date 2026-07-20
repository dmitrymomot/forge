package formula

import (
	"fmt"

	"github.com/dmitrymomot/forge/core/decimal"
)

// Compiled is a validated, fingerprinted spec with all stage references
// resolved and registered functions bound, ready for repeated Eval calls.
// Compile once per spec and reuse — a Compiled is immutable and safe for
// concurrent use.
type Compiled struct {
	stageIndex  map[string]int
	fingerprint string
	spec        Spec
	stages      []compiledStage
}

// ref points a term or arg at its value source: a prior stage by index, or —
// when stage is -1 — an input looked up by name at Eval time.
type ref struct {
	input string
	stage int
}

type compiledTerm struct {
	ref         ref
	coefficient decimal.Decimal
}

type compiledStage struct {
	fn       Func
	clamp    *Clamp
	round    *Round
	name     string
	fnName   string
	terms    []compiledTerm
	argNames []string
	args     []ref
}

// Compile validates spec, binds every func stage to a function registered via
// WithFunc, resolves term and arg references, and computes the fingerprint.
// The spec is deep-copied, so later mutation of the caller's value cannot
// desync the fingerprint from what evaluates. Errors wrap ErrInvalidSpec,
// ErrInvalidFunc, or ErrUnknownFunc.
func Compile(spec Spec, opts ...Option) (*Compiled, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	funcs := make(map[string]Func, len(cfg.funcs))
	for _, nf := range cfg.funcs {
		if nf.name == "" {
			return nil, fmt.Errorf("%w: empty name", ErrInvalidFunc)
		}
		if nf.fn == nil {
			return nil, fmt.Errorf("%w: nil func %q", ErrInvalidFunc, nf.name)
		}
		if _, dup := funcs[nf.name]; dup {
			return nil, fmt.Errorf("%w: duplicate func %q", ErrInvalidFunc, nf.name)
		}
		funcs[nf.name] = nf.fn
	}

	spec = spec.clone()
	c := &Compiled{
		spec:        spec,
		fingerprint: spec.Fingerprint(),
		stages:      make([]compiledStage, len(spec.Stages)),
		stageIndex:  make(map[string]int, len(spec.Stages)),
	}
	for i, st := range spec.Stages {
		c.stageIndex[st.Name] = i
	}
	for i, st := range spec.Stages {
		cs := compiledStage{name: st.Name, clamp: st.Clamp, round: st.Round}
		if st.Func != "" {
			fn, ok := funcs[st.Func]
			if !ok {
				return nil, fmt.Errorf("%w: stage %q references %q", ErrUnknownFunc, st.Name, st.Func)
			}
			cs.fnName = st.Func
			cs.fn = fn
			cs.argNames = st.Args
			cs.args = make([]ref, len(st.Args))
			for j, a := range st.Args {
				cs.args[j] = c.resolve(a)
			}
		} else {
			cs.terms = make([]compiledTerm, len(st.Terms))
			for j, t := range st.Terms {
				cs.terms[j] = compiledTerm{ref: c.resolve(t.Metric), coefficient: t.Coefficient}
			}
		}
		c.stages[i] = cs
	}
	return c, nil
}

// resolve maps a metric name to its source. Validate already rejected self and
// forward stage references, so any stage-name hit here is a prior stage;
// everything else must arrive as an input at Eval time.
func (c *Compiled) resolve(metric string) ref {
	if i, isStage := c.stageIndex[metric]; isStage {
		return ref{stage: i}
	}
	return ref{stage: -1, input: metric}
}

// Spec returns a deep copy of the compiled spec, so callers can serialize or
// inspect it without being able to mutate what evaluates.
func (c *Compiled) Spec() Spec { return c.spec.clone() }

// Version returns the compiled spec's version.
func (c *Compiled) Version() string { return c.spec.Version }

// Fingerprint returns the compiled spec's fingerprint (see Spec.Fingerprint).
func (c *Compiled) Fingerprint() string { return c.fingerprint }
