package featureflag

import (
	"fmt"
	"slices"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// Flag is a serializable flag definition. The zero value is disabled
// (fail-safe): getters return the caller's default until Enabled is set.
type Flag struct {
	Value   string   // canonical string form; typed getters coerce via typeconv
	Allow   []string // tokens: any match with the subject's token set → always on
	Deny    []string // tokens: any match → always off (wins over Allow)
	Rollout int      // 0-100 percent of subjects; 100 = everyone
	Enabled bool     // kill switch; false → getters return the caller default
}

// Flags is a flag set keyed by flag name. It embeds directly in an
// application config struct loaded by ops/config.
type Flags map[string]Flag

// UnmarshalYAML accepts either scalar shorthand (dark_mode: true) or object
// form ({value, enabled, rollout, allow, deny}). Implemented via the legacy
// func-style unmarshaler so this package does not import yaml.v3.
func (f *Flag) UnmarshalYAML(unmarshal func(any) error) error {
	var m map[string]any
	if err := unmarshal(&m); err == nil {
		return f.fromMap(m)
	}
	var scalar any
	if err := unmarshal(&scalar); err != nil {
		return err
	}
	return f.fromScalar(scalar)
}

func (f *Flag) fromMap(m map[string]any) error {
	out := Flag{Enabled: true, Rollout: 100}
	for k, v := range m {
		switch k {
		case "value":
			if !isScalar(v) {
				return fmt.Errorf("%w: value must be a scalar", ErrInvalidFlag)
			}
			out.Value = typeconv.Format(v)
		case "enabled":
			b, ok := v.(bool)
			if !ok {
				return fmt.Errorf("%w: enabled must be a bool", ErrInvalidFlag)
			}
			out.Enabled = b
		case "rollout":
			n, ok := toInt(v)
			if !ok {
				return fmt.Errorf("%w: rollout must be an integer", ErrInvalidFlag)
			}
			if n < 0 || n > 100 {
				return fmt.Errorf("%w: got %d", ErrInvalidRollout, n)
			}
			out.Rollout = n
		case "allow":
			ts, err := tokenList(v)
			if err != nil {
				return fmt.Errorf("allow: %w", err)
			}
			out.Allow = ts
		case "deny":
			ts, err := tokenList(v)
			if err != nil {
				return fmt.Errorf("deny: %w", err)
			}
			out.Deny = ts
		default:
			return fmt.Errorf("%w: unknown field %q", ErrInvalidFlag, k)
		}
	}
	*f = out
	return nil
}

func (f *Flag) fromScalar(v any) error {
	if !isScalar(v) {
		return fmt.Errorf("%w: flag must be a scalar or a mapping", ErrInvalidFlag)
	}
	*f = Flag{Value: typeconv.Format(v), Enabled: true, Rollout: 100}
	return nil
}

// toInt normalizes the numeric kinds yaml.v3 (or other decoders) may produce
// for an integer node. Deliberately excludes float64/string: a float rollout
// is a malformed flag, not a value to coerce.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	default:
		return 0, false
	}
}

func isScalar(v any) bool {
	switch v.(type) {
	case bool, int, int64, uint64, float64, string:
		return true
	default:
		return false
	}
}

func tokenList(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: token list must be a sequence of strings", ErrInvalidFlag)
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("%w: token must be a string", ErrInvalidFlag)
		}
		if s == "" {
			return nil, fmt.Errorf("%w: empty token", ErrInvalidFlag)
		}
		out = append(out, s)
	}
	return out, nil
}

// UnmarshalYAML validates keys while delegating per-flag parsing to Flag.
//
// It decodes into map[string]any (not map[string]Flag) and dispatches to
// Flag.fromMap/fromScalar itself: yaml.v3 never invokes a custom
// UnmarshalYAML for a null node (it silently leaves the zero value), so
// relying on nested dispatch would let a null flag definition (e.g. "f:")
// through unnoticed instead of reporting ErrInvalidFlag.
func (fs *Flags) UnmarshalYAML(unmarshal func(any) error) error {
	var m map[string]any
	if err := unmarshal(&m); err != nil {
		return err
	}
	out := make(Flags, len(m))
	for k, v := range m {
		if k == "" {
			return ErrEmptyKey
		}
		if v == nil {
			return fmt.Errorf("%w: flag %q is null", ErrInvalidFlag, k)
		}
		var f Flag
		if mv, ok := v.(map[string]any); ok {
			if err := f.fromMap(mv); err != nil {
				return err
			}
		} else if err := f.fromScalar(v); err != nil {
			return err
		}
		out[k] = f
	}
	*fs = out
	return nil
}

// clone deep-copies the set so a Client stays immutable after New.
func (fs Flags) clone() Flags {
	out := make(Flags, len(fs))
	for k, f := range fs {
		f.Allow = slices.Clone(f.Allow)
		f.Deny = slices.Clone(f.Deny)
		out[k] = f
	}
	return out
}
