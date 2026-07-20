package formula

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/dmitrymomot/forge/core/decimal"
)

// Spec is a formula as structured, versioned data: an ordered list of named
// stages, each deriving one metric from inputs and prior stages. A spec is
// plain data — it JSON-round-trips for storage and carries no behavior beyond
// Validate and Fingerprint. Specs are immutable once referenced by an
// evaluation: store them verbatim and mint a new Version for any change, so
// recomputing an old statement byte-matches the original.
type Spec struct {
	// Version identifies this exact spec revision and is stamped into every
	// Result. Required.
	Version string `json:"version"`
	// Stages are evaluated in order; each may reference inputs and the stages
	// before it, never itself or later ones.
	Stages []Stage `json:"stages"`
}

// Stage derives one named metric. Exactly one of Terms (a linear combination)
// or Func (a registered Go function) must be set. The raw result is then
// rounded (if Round is set) and clamped (if Clamp is set), in that order, so
// the final value always honors the clamp bounds exactly.
type Stage struct {
	// Clamp optionally bounds the stage value after rounding.
	Clamp *Clamp `json:"clamp,omitempty"`
	// Round optionally pins the stage value to a fixed scale before clamping.
	Round *Round `json:"round,omitempty"`
	// Name is the metric this stage derives. Unique within the spec; later
	// stages reference it by this name.
	Name string `json:"name"`
	// Func names a registered Go function (see WithFunc) for shapes beyond
	// staged linear terms. Mutually exclusive with Terms.
	Func string `json:"func,omitempty"`
	// Terms is the linear form: the stage value is the exact sum of
	// coefficient × metric over all terms.
	Terms []Term `json:"terms,omitempty"`
	// Args are the metric names resolved and passed to Func, in order. Only
	// valid with Func.
	Args []string `json:"args,omitempty"`
}

// Term is one linear contribution: Coefficient × the value of Metric, where
// Metric names an input or a prior stage. Multiplication is exact — no
// rounding happens inside a term.
type Term struct {
	Metric      string          `json:"metric"`
	Coefficient decimal.Decimal `json:"coefficient"`
}

// Clamp bounds a stage value: values below Min become Min, values above Max
// become Max. At least one bound must be set; when both are, Min ≤ Max.
type Clamp struct {
	Min *decimal.Decimal `json:"min,omitempty"`
	Max *decimal.Decimal `json:"max,omitempty"`
}

// Round pins a stage value to Scale fractional digits using Mode. The zero
// Mode is decimal.HalfEven (banker's rounding).
type Round struct {
	Scale int32                `json:"scale"`
	Mode  decimal.RoundingMode `json:"mode"`
}

// Validate checks the spec's structure: version and stages present, stage
// names unique and non-empty, exactly one of terms/func per stage, args only
// with func, no reference to the stage itself or a later stage, clamps
// non-empty with min ≤ max, and round scale/mode in range. Whether a func is
// registered and whether inputs exist are checked later, by Compile and Eval.
// All failures wrap ErrInvalidSpec.
func (s Spec) Validate() error {
	if s.Version == "" {
		return fmt.Errorf("%w: version is required", ErrInvalidSpec)
	}
	if len(s.Stages) == 0 {
		return fmt.Errorf("%w: at least one stage is required", ErrInvalidSpec)
	}
	order := make(map[string]int, len(s.Stages))
	for i, st := range s.Stages {
		if st.Name == "" {
			return fmt.Errorf("%w: stage %d: name is required", ErrInvalidSpec, i)
		}
		if _, dup := order[st.Name]; dup {
			return fmt.Errorf("%w: duplicate stage %q", ErrInvalidSpec, st.Name)
		}
		order[st.Name] = i
	}
	for i, st := range s.Stages {
		if err := validateStage(st, i, order); err != nil {
			return err
		}
	}
	return nil
}

func validateStage(st Stage, idx int, order map[string]int) error {
	linear, fn := len(st.Terms) > 0, st.Func != ""
	switch {
	case linear && fn:
		return fmt.Errorf("%w: stage %q: terms and func are mutually exclusive", ErrInvalidSpec, st.Name)
	case !linear && !fn:
		return fmt.Errorf("%w: stage %q: terms or func is required", ErrInvalidSpec, st.Name)
	case linear && len(st.Args) > 0:
		return fmt.Errorf("%w: stage %q: args are only valid with func", ErrInvalidSpec, st.Name)
	}
	for _, t := range st.Terms {
		if t.Metric == "" {
			return fmt.Errorf("%w: stage %q: term metric is required", ErrInvalidSpec, st.Name)
		}
		if j, isStage := order[t.Metric]; isStage && j >= idx {
			return fmt.Errorf("%w: stage %q: term references %q before it is computed", ErrInvalidSpec, st.Name, t.Metric)
		}
	}
	for _, a := range st.Args {
		if a == "" {
			return fmt.Errorf("%w: stage %q: arg metric is required", ErrInvalidSpec, st.Name)
		}
		if j, isStage := order[a]; isStage && j >= idx {
			return fmt.Errorf("%w: stage %q: arg references %q before it is computed", ErrInvalidSpec, st.Name, a)
		}
	}
	if st.Clamp != nil {
		if st.Clamp.Min == nil && st.Clamp.Max == nil {
			return fmt.Errorf("%w: stage %q: clamp needs min or max", ErrInvalidSpec, st.Name)
		}
		if st.Clamp.Min != nil && st.Clamp.Max != nil && st.Clamp.Min.Cmp(*st.Clamp.Max) > 0 {
			return fmt.Errorf("%w: stage %q: clamp min > max", ErrInvalidSpec, st.Name)
		}
	}
	if st.Round != nil {
		if st.Round.Scale < 0 {
			return fmt.Errorf("%w: stage %q: negative round scale", ErrInvalidSpec, st.Name)
		}
		if st.Round.Mode < decimal.HalfEven || st.Round.Mode > decimal.Floor {
			return fmt.Errorf("%w: stage %q: unknown rounding mode %d", ErrInvalidSpec, st.Name, int(st.Round.Mode))
		}
	}
	return nil
}

// Fingerprint returns the audit anchor: lowercase-hex SHA-256 over the spec's
// canonical binary encoding (every field length-prefixed, decimals in their
// scale-preserving String form, in declaration order). Any byte-level change —
// including a numerically equal coefficient at a different scale, or
// reordered stages or terms — yields a different fingerprint; that
// representation-sensitivity is the point. The encoding is frozen: it never
// changes with Go or library versions, so stored fingerprints stay verifiable
// forever.
func (s Spec) Fingerprint() string {
	h := sha256.New()
	writeStr(h, s.Version)
	writeLen(h, len(s.Stages))
	for _, st := range s.Stages {
		writeStr(h, st.Name)
		writeLen(h, len(st.Terms))
		for _, t := range st.Terms {
			writeStr(h, t.Metric)
			writeStr(h, t.Coefficient.String())
		}
		writeStr(h, st.Func)
		writeLen(h, len(st.Args))
		for _, a := range st.Args {
			writeStr(h, a)
		}
		if st.Clamp == nil {
			h.Write([]byte{0})
		} else {
			h.Write([]byte{1})
			writeOptDecimal(h, st.Clamp.Min)
			writeOptDecimal(h, st.Clamp.Max)
		}
		if st.Round == nil {
			h.Write([]byte{0})
		} else {
			h.Write([]byte{1})
			writeLen(h, int(st.Round.Scale))
			writeLen(h, int(st.Round.Mode))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeLen(h hash.Hash, n int) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(n))
	h.Write(buf[:])
}

func writeStr(h hash.Hash, s string) {
	writeLen(h, len(s))
	h.Write([]byte(s))
}

func writeOptDecimal(h hash.Hash, d *decimal.Decimal) {
	if d == nil {
		h.Write([]byte{0})
		return
	}
	h.Write([]byte{1})
	writeStr(h, d.String())
}

// clone returns a deep copy sharing no mutable memory with s, so a Compiled
// spec cannot be changed underneath its fingerprint.
func (s Spec) clone() Spec {
	out := Spec{Version: s.Version}
	if s.Stages == nil {
		return out
	}
	out.Stages = make([]Stage, len(s.Stages))
	for i, st := range s.Stages {
		c := Stage{Name: st.Name, Func: st.Func}
		if st.Terms != nil {
			c.Terms = make([]Term, len(st.Terms))
			copy(c.Terms, st.Terms)
		}
		if st.Args != nil {
			c.Args = make([]string, len(st.Args))
			copy(c.Args, st.Args)
		}
		if st.Clamp != nil {
			cl := Clamp{}
			if st.Clamp.Min != nil {
				cl.Min = new(*st.Clamp.Min)
			}
			if st.Clamp.Max != nil {
				cl.Max = new(*st.Clamp.Max)
			}
			c.Clamp = &cl
		}
		if st.Round != nil {
			c.Round = new(*st.Round)
		}
		out.Stages[i] = c
	}
	return out
}
