package structfields

import (
	"fmt"
	"reflect"
	"time"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// SetString parses raw according to f's field type and assigns it via f.Set.
// Supported: string, bool, all int/uint widths, float32/64, time.Duration,
// time.Time (RFC3339), and []string (comma-separated). Other kinds return
// ErrUnsupportedKind. A read-only (value-struct) field returns ErrNotSettable
// through f.Set; a parse failure surfaces typeconv.ErrSyntax.
func SetString(f Field, raw string) error {
	switch f.Value.Type() {
	case reflect.TypeFor[time.Duration]():
		d, err := typeconv.ParseDuration(raw)
		if err != nil {
			return err
		}
		return f.Set(d)
	case reflect.TypeFor[time.Time]():
		tm, err := typeconv.ParseTime(raw)
		if err != nil {
			return err
		}
		return f.Set(tm)
	}

	switch f.Value.Kind() {
	case reflect.String:
		return f.Set(raw)
	case reflect.Bool:
		v, err := typeconv.ParseBool(raw)
		if err != nil {
			return err
		}
		return f.Set(v)
	case reflect.Int:
		return setSigned[int](f, raw)
	case reflect.Int8:
		return setSigned[int8](f, raw)
	case reflect.Int16:
		return setSigned[int16](f, raw)
	case reflect.Int32:
		return setSigned[int32](f, raw)
	case reflect.Int64:
		return setSigned[int64](f, raw)
	case reflect.Uint:
		return setUnsigned[uint](f, raw)
	case reflect.Uint8:
		return setUnsigned[uint8](f, raw)
	case reflect.Uint16:
		return setUnsigned[uint16](f, raw)
	case reflect.Uint32:
		return setUnsigned[uint32](f, raw)
	case reflect.Uint64:
		return setUnsigned[uint64](f, raw)
	case reflect.Uintptr:
		return setUnsigned[uintptr](f, raw)
	case reflect.Float32:
		return setFloat[float32](f, raw)
	case reflect.Float64:
		return setFloat[float64](f, raw)
	case reflect.Slice:
		if f.Value.Type().Elem().Kind() == reflect.String {
			v, err := typeconv.ParseSlice[string](raw, ",")
			if err != nil {
				return err
			}
			return f.Set(v)
		}
	}
	return fmt.Errorf("structfields: field %q: %w (%s)", f.Name, ErrUnsupportedKind, f.Value.Kind())
}

func setSigned[T int | int8 | int16 | int32 | int64](f Field, raw string) error {
	v, err := typeconv.ParseInt[T](raw)
	if err != nil {
		return err
	}
	return f.Set(v)
}

func setUnsigned[T uint | uint8 | uint16 | uint32 | uint64 | uintptr](f Field, raw string) error {
	v, err := typeconv.ParseUint[T](raw)
	if err != nil {
		return err
	}
	return f.Set(v)
}

func setFloat[T float32 | float64](f Field, raw string) error {
	v, err := typeconv.ParseFloat[T](raw)
	if err != nil {
		return err
	}
	return f.Set(v)
}
