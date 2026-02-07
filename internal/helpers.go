package internal

import "strconv"

func ContextValue[T any](c Context, key any) T {
	if v, ok := c.Get(key).(T); ok {
		return v
	}
	var zero T
	return zero
}

func Param[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string) T {
	v, _ := convertParam[T](c.Param(name))
	return v
}

func Query[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string) T {
	v, _ := convertParam[T](c.Query(name))
	return v
}

// QueryDefault retrieves a typed query parameter with a default value.
// Returns defaultValue if the parameter is empty or cannot be parsed.
func QueryDefault[T ~string | ~int | ~int64 | ~float64 | ~bool](c Context, name string, defaultValue T) T {
	raw := c.Query(name)
	if raw == "" {
		return defaultValue
	}
	v, ok := convertParam[T](raw)
	if !ok {
		return defaultValue
	}
	return v
}

// convertParam converts a raw string to the target type T.
// Returns the converted value and true on success, or the zero value and false on failure.
func convertParam[T ~string | ~int | ~int64 | ~float64 | ~bool](raw string) (T, bool) {
	var zero T
	switch any(zero).(type) {
	case string:
		return any(raw).(T), true
	case int:
		v, err := strconv.Atoi(raw)
		if err != nil {
			return zero, false
		}
		return any(v).(T), true
	case int64:
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return zero, false
		}
		return any(v).(T), true
	case float64:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return zero, false
		}
		return any(v).(T), true
	case bool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return zero, false
		}
		return any(v).(T), true
	}
	return zero, false
}
