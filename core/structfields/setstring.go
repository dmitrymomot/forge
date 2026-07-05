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
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := typeconv.ParseInt[int64](raw)
		if err != nil {
			return err
		}
		return f.Set(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := typeconv.ParseUint[uint64](raw)
		if err != nil {
			return err
		}
		return f.Set(v)
	case reflect.Float32, reflect.Float64:
		v, err := typeconv.ParseFloat[float64](raw)
		if err != nil {
			return err
		}
		return f.Set(v)
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
