package validate

import (
	"sort"
	"strings"
)

// Result is one field's violations; nil when the field is clean.
type Result []Violation

// Apply runs each rule against value, tags every failing (non-zero) violation with
// the field name, drops the passing ones, and returns nil when all pass.
func Apply[T any](field string, value T, rules ...Rule[T]) Result {
	var out Result
	for _, r := range rules {
		if v := r(value); !v.IsZero() {
			v.Field = field
			out = append(out, v)
		}
	}
	return out
}

// Manual injects a literal violation for a field (e.g. after a DB "email taken" check).
func Manual(field, message string) Result {
	return Result{{Field: field, Message: message}}
}

// ManualKey injects a keyed violation for a field.
func ManualKey(field, key string, params ...Param) Result {
	return Result{{Field: field, Key: key, Params: params}}
}

// Check flattens field results into one error, or untyped nil when everything is clean.
func Check(results ...Result) error {
	var all Errors
	for _, r := range results {
		all = append(all, r...)
	}
	if len(all) == 0 {
		return nil
	}
	return all
}

// Errors is the aggregated failure set Check returns.
type Errors []Violation

// Error renders a sorted single line (log-friendly): "field: message; …".
func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, v := range e {
		parts[i] = v.Field + ": " + v.String()
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// ByField groups violations by field for API responses (built on demand).
func (e Errors) ByField() map[string][]Violation {
	m := make(map[string][]Violation, len(e))
	for _, v := range e {
		m[v.Field] = append(m[v.Field], v)
	}
	return m
}
