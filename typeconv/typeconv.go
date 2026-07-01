package typeconv

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type float interface {
	~float32 | ~float64
}

func syntax(err error) error { return fmt.Errorf("%w: %v", ErrSyntax, err) }

// Parse converts s into T. Supported T are the base kinds string, bool, the
// sized int/uint kinds, float32/64, time.Duration, and time.Time. A defined
// type does not match; use the constraint helpers or an explicit conversion.
func Parse[T any](s string) (T, error) {
	var zero T
	var out any
	var err error
	switch any(zero).(type) {
	case string:
		out = s
	case bool:
		out, err = strconv.ParseBool(s)
	case int:
		var v int64
		v, err = strconv.ParseInt(s, 10, strconv.IntSize)
		out = int(v)
	case int8:
		var v int64
		v, err = strconv.ParseInt(s, 10, 8)
		out = int8(v)
	case int16:
		var v int64
		v, err = strconv.ParseInt(s, 10, 16)
		out = int16(v)
	case int32:
		var v int64
		v, err = strconv.ParseInt(s, 10, 32)
		out = int32(v)
	case int64:
		out, err = strconv.ParseInt(s, 10, 64)
	case uint:
		var v uint64
		v, err = strconv.ParseUint(s, 10, strconv.IntSize)
		out = uint(v)
	case uint8:
		var v uint64
		v, err = strconv.ParseUint(s, 10, 8)
		out = uint8(v)
	case uint16:
		var v uint64
		v, err = strconv.ParseUint(s, 10, 16)
		out = uint16(v)
	case uint32:
		var v uint64
		v, err = strconv.ParseUint(s, 10, 32)
		out = uint32(v)
	case uint64:
		out, err = strconv.ParseUint(s, 10, 64)
	case float32:
		var v float64
		v, err = strconv.ParseFloat(s, 32)
		out = float32(v)
	case float64:
		out, err = strconv.ParseFloat(s, 64)
	case time.Duration:
		out, err = time.ParseDuration(s)
	case time.Time:
		out, err = time.Parse(time.RFC3339, s)
	default:
		return zero, fmt.Errorf("%w: %T", ErrUnsupportedType, zero)
	}
	if err != nil {
		return zero, syntax(err)
	}
	return out.(T), nil
}

// Format is the lossless inverse of Parse for the supported scalar set. Any
// other type is rendered with fmt.Sprint.
func Format(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case uintptr:
		return strconv.FormatUint(uint64(x), 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Duration:
		return x.String()
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprint(v)
	}
}

// ParseBool parses a boolean via strconv.ParseBool.
func ParseBool(s string) (bool, error) {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, syntax(err)
	}
	return v, nil
}

// ParseInt parses a signed integer of any width into T, rejecting values that
// overflow T (detected by a narrowing round-trip).
func ParseInt[T signed](s string) (T, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, syntax(err)
	}
	r := T(v)
	if int64(r) != v {
		return 0, fmt.Errorf("%w: %d overflows target type", ErrSyntax, v)
	}
	return r, nil
}

// ParseUint parses an unsigned integer of any width into T, rejecting overflow.
func ParseUint[T unsigned](s string) (T, error) {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, syntax(err)
	}
	r := T(v)
	if uint64(r) != v {
		return 0, fmt.Errorf("%w: %d overflows target type", ErrSyntax, v)
	}
	return r, nil
}

// ParseFloat parses a float into T. float32 out-of-range parses to ±Inf per
// strconv; width range is best-effort.
func ParseFloat[T float](s string) (T, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, syntax(err)
	}
	return T(v), nil
}

// ParseDuration parses a Go duration string ("1h30m").
func ParseDuration(s string) (time.Duration, error) {
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, syntax(err)
	}
	return v, nil
}

// ParseTime parses an RFC3339 timestamp.
func ParseTime(s string) (time.Time, error) {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, syntax(err)
	}
	return v, nil
}

// ParseSlice splits s on sep and Parse[T]s each element. It trims whitespace
// around the whole input and each element, drops empty-after-trim elements
// (so "1, 2, 3," yields [1 2 3]), and returns nil for empty input.
func ParseSlice[T any](s, sep string) ([]T, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, sep)
	out := make([]T, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := Parse[T](p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
