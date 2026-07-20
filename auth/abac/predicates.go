package abac

import (
	"context"

	"github.com/dmitrymomot/forge/auth/access"
)

// Attr reads a typed attribute from a Subject/Resource attribute bag. Nil-safe;
// ok is false when the key is absent or the value is not a T.
func Attr[T any](attrs map[string]any, key string) (T, bool) {
	v, ok := attrs[key]
	if !ok {
		var zero T
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}

// Owner is the most common relationship predicate: true when the resource's
// attrKey string attribute equals the subject ID and both are non-empty.
// Consumers populate the attribute when describing the resource
// (Attrs: map[string]any{"owner_id": doc.OwnerID}).
func Owner(attrKey string) Predicate {
	return func(_ context.Context, s access.Subject, r access.Resource) (bool, error) {
		owner, ok := Attr[string](r.Attrs, attrKey)
		return ok && owner != "" && owner == s.ID, nil
	}
}

// And is true when every predicate is true; it short-circuits on the first
// false. With no predicates it is false (fail closed — an empty conjunction
// must not grant). An error stops evaluation and propagates. Nil predicates
// panic here at wiring time rather than at request time.
func And(preds ...Predicate) Predicate {
	requireNonNil("And", preds)
	return func(ctx context.Context, s access.Subject, r access.Resource) (bool, error) {
		if len(preds) == 0 {
			return false, nil
		}
		for _, p := range preds {
			ok, err := p(ctx, s, r)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
}

// Or is true when any predicate is true; it short-circuits on the first true.
// With no predicates it is false. An error stops evaluation and propagates
// (fail closed) even if a later predicate would have been true. Nil predicates
// panic here at wiring time rather than at request time.
func Or(preds ...Predicate) Predicate {
	requireNonNil("Or", preds)
	return func(ctx context.Context, s access.Subject, r access.Resource) (bool, error) {
		for _, p := range preds {
			ok, err := p(ctx, s, r)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
}

// Not inverts pred. An error propagates with a false result (fail closed), not
// an inverted one. A nil pred panics here at wiring time.
func Not(pred Predicate) Predicate {
	if pred == nil {
		panic("abac: Not requires a non-nil predicate")
	}
	return func(ctx context.Context, s access.Subject, r access.Resource) (bool, error) {
		ok, err := pred(ctx, s, r)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}
}

func requireNonNil(combinator string, preds []Predicate) {
	for _, p := range preds {
		if p == nil {
			panic("abac: " + combinator + " requires non-nil predicates")
		}
	}
}
