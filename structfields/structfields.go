package structfields

import (
	"fmt"
	"reflect"
)

// Field is one exported struct field surfaced by Walk.
type Field struct {
	Set   func(v any) error
	Value reflect.Value // settable when Walk received a non-nil *struct
	Name  string        // Go field name
	Tag   Tag           // parsed tagKey tag
}

// Walk visits each exported field of a struct (or non-nil *struct) exactly
// once, in declaration order, invoking fn with a Field carrying the field's
// name, its parsed tagKey tag, its reflect.Value, and a Set closure.
//
// A non-nil *struct yields settable fields; a value struct is read-only and
// each Field.Set returns ErrNotSettable. Anything that is not a struct or
// non-nil *struct returns ErrNotStruct. Traversal is shallow: an anonymous
// embedded struct is yielded as a single field, not flattened. Unexported
// fields are skipped. fn's error stops the walk and is returned as-is.
func Walk(v any, tagKey string, fn func(Field) error) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return ErrNotStruct
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return ErrNotStruct
	}

	rt := rv.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}

		fieldVal := rv.Field(i)
		field := Field{
			Name:  sf.Name,
			Tag:   parseTag(sf.Tag.Get(tagKey)),
			Value: fieldVal,
			Set:   makeSetter(sf.Name, fieldVal),
		}
		if err := fn(field); err != nil {
			return err
		}
	}
	return nil
}

// makeSetter returns a closure that assigns/converts v into target, or
// ErrNotSettable when target cannot be set (value-struct walk).
func makeSetter(name string, target reflect.Value) func(v any) error {
	return func(v any) error {
		if !target.CanSet() {
			return fmt.Errorf("structfields: field %q: %w", name, ErrNotSettable)
		}
		in := reflect.ValueOf(v)
		if !in.IsValid() {
			return fmt.Errorf("structfields: field %q: cannot set from nil", name)
		}
		tt := target.Type()
		switch {
		case in.Type().AssignableTo(tt):
			target.Set(in)
		case in.Type().ConvertibleTo(tt):
			// reflect reports slice->array (and slice->*array) as convertible,
			// but Convert PANICS when the slice length differs from the array
			// length. Guard that case so Set returns an error instead.
			if arr := arrayLen(tt); arr >= 0 && in.Kind() == reflect.Slice && in.Len() != arr {
				return fmt.Errorf("structfields: field %q: cannot set %s from %s: length mismatch", name, tt, in.Type())
			}
			target.Set(in.Convert(tt))
		default:
			return fmt.Errorf("structfields: field %q: cannot set %s from %s", name, tt, in.Type())
		}
		return nil
	}
}

// arrayLen returns the length of t if t is an array or a pointer to an array,
// or -1 otherwise. Used to reject slice->array conversions whose lengths
// differ before reflect.Value.Convert panics on them.
func arrayLen(t reflect.Type) int {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Array {
		return t.Len()
	}
	return -1
}
