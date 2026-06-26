package request

import (
	"encoding"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parse converts a single non-empty raw string into T. Resolution order: built-in
// scalars via a type switch on any(&v); *time.Duration via time.ParseDuration; then
// any type whose pointer implements encoding.TextUnmarshaler (time.Time, uuid.UUID,
// netip.Addr, custom enums). No reflect package: interface assertions only.
func parse[T any](raw string) (T, error) {
	var v T
	switch p := any(&v).(type) {
	case *string:
		*p = raw
	case *bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return v, err
		}
		*p = b
	case *int:
		n, err := strconv.ParseInt(raw, 10, strconv.IntSize)
		if err != nil {
			return v, err
		}
		*p = int(n)
	case *int8:
		n, err := strconv.ParseInt(raw, 10, 8)
		if err != nil {
			return v, err
		}
		*p = int8(n)
	case *int16:
		n, err := strconv.ParseInt(raw, 10, 16)
		if err != nil {
			return v, err
		}
		*p = int16(n)
	case *int32:
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return v, err
		}
		*p = int32(n)
	case *int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return v, err
		}
		*p = n
	case *uint:
		n, err := strconv.ParseUint(raw, 10, strconv.IntSize)
		if err != nil {
			return v, err
		}
		*p = uint(n)
	case *uint8:
		n, err := strconv.ParseUint(raw, 10, 8)
		if err != nil {
			return v, err
		}
		*p = uint8(n)
	case *uint16:
		n, err := strconv.ParseUint(raw, 10, 16)
		if err != nil {
			return v, err
		}
		*p = uint16(n)
	case *uint32:
		n, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return v, err
		}
		*p = uint32(n)
	case *uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return v, err
		}
		*p = n
	case *float32:
		f, err := strconv.ParseFloat(raw, 32)
		if err != nil {
			return v, err
		}
		*p = float32(f)
	case *float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return v, err
		}
		*p = f
	case *time.Duration:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return v, err
		}
		*p = d
	default:
		if tu, ok := any(&v).(encoding.TextUnmarshaler); ok {
			if err := tu.UnmarshalText([]byte(raw)); err != nil {
				return v, err
			}
			return v, nil
		}
		return v, fmt.Errorf("unsupported type %T", v)
	}
	return v, nil
}

// resolve applies the missing/default/malformed contract for a single value: an
// empty raw string yields def[0] (or the zero value) and a nil error; a non-empty
// value is parsed by p and any failure is wrapped as *Error.
func resolve[T any](src Source, key, raw string, p func(string) (T, error), def []T) (T, error) {
	if raw == "" {
		if len(def) > 0 {
			return def[0], nil
		}
		var zero T
		return zero, nil
	}
	v, err := p(raw)
	if err != nil {
		var zero T
		return zero, &Error{Source: src, Key: key, Kind: KindMalformed, Err: err}
	}
	return v, nil
}

// resolveSlice parses each non-empty element of raws with p. Any failure wraps as
// *Error; an all-empty result falls back to def[0] (or nil).
func resolveSlice[T any](src Source, key string, raws []string, p func(string) (T, error), def [][]T) ([]T, error) {
	out := make([]T, 0, len(raws))
	for _, raw := range raws {
		if raw == "" {
			continue
		}
		v, err := p(raw)
		if err != nil {
			return nil, &Error{Source: src, Key: key, Kind: KindMalformed, Err: err}
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		if len(def) > 0 {
			return def[0], nil
		}
		return nil, nil
	}
	return out, nil
}

// resolveSplit splits raw on sep, trims and skips empty elements, then parses each
// with p. An empty raw falls back to def[0] (or nil).
func resolveSplit[T any](src Source, key, raw, sep string, p func(string) (T, error), def [][]T) ([]T, error) {
	if raw == "" {
		if len(def) > 0 {
			return def[0], nil
		}
		return nil, nil
	}
	var out []T
	for _, part := range strings.Split(raw, sep) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := p(part)
		if err != nil {
			return nil, &Error{Source: src, Key: key, Kind: KindMalformed, Err: err}
		}
		out = append(out, v)
	}
	return out, nil
}
