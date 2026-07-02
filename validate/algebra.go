package validate

import (
	"fmt"
	"strings"
)

// And combines rules into one that reports the FIRST failure (short-circuit).
func And[T any](rules ...Rule[T]) Rule[T] {
	return func(val T) Violation {
		for _, r := range rules {
			if v := r(val); !v.IsZero() {
				return v
			}
		}
		return Violation{}
	}
}

// Or passes if ANY sub-rule passes; otherwise reports a violation with key.
func Or[T any](key string, rules ...Rule[T]) Rule[T] {
	return func(val T) Violation {
		for _, r := range rules {
			if r(val).IsZero() {
				return Violation{}
			}
		}
		return Violation{Key: key}
	}
}

// Not inverts r: it fails (with key) when r passes, and passes when r fails.
func Not[T any](r Rule[T], key string) Rule[T] {
	return func(val T) Violation {
		if r(val).IsZero() {
			return Violation{Key: key}
		}
		return Violation{}
	}
}

// Each applies r to every element; on the first failure it returns that element's
// Violation with an added {index} param.
func Each[T any](r Rule[T]) Rule[[]T] {
	return func(items []T) Violation {
		for i, it := range items {
			if v := r(it); !v.IsZero() {
				v.Params = append(v.Params, Param{Key: "index", Value: i})
				return v
			}
		}
		return Violation{}
	}
}

// Msg wraps r: on failure it interpolates the violation's Params into {name}
// placeholders and stores the result as Message (a literal that bypasses i18n).
func Msg[T any](r Rule[T], template string) Rule[T] {
	return func(val T) Violation {
		v := r(val)
		if v.IsZero() {
			return v
		}
		v.Message = interpolate(template, v.Params)
		return v
	}
}

// WithKey wraps r: on failure it swaps the i18n Key, preserving Params.
func WithKey[T any](r Rule[T], key string) Rule[T] {
	return func(val T) Violation {
		v := r(val)
		if v.IsZero() {
			return v
		}
		v.Key = key
		return v
	}
}

// When returns r when cond holds, else a rule that always passes — so the guarded
// rule is only EVALUATED when cond is true.
func When[T any](cond bool, r Rule[T]) Rule[T] {
	if cond {
		return r
	}
	return func(T) Violation { return Violation{} }
}

// WhenField includes the field results only when cond holds; else nil.
func WhenField(cond bool, results ...Result) Result {
	if !cond {
		return nil
	}
	var out Result
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}

// interpolate replaces {name} placeholders with fmt.Sprint(Params[name]); unknown
// placeholders are left verbatim. Dependency-free, no i18n layer needed.
func interpolate(tpl string, params []Param) string {
	if len(params) == 0 {
		return tpl
	}
	out := tpl
	for _, p := range params {
		out = strings.ReplaceAll(out, "{"+p.Key+"}", fmt.Sprint(p.Value))
	}
	return out
}
