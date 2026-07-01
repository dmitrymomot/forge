package validate

import "cmp"

// Local unexported constraints (mirrors typeconv; avoids x/exp/constraints).
type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}
type number interface {
	integer | ~float32 | ~float64
}

// isNaN reports whether v is a floating-point NaN. NaN is the only value that is
// unequal to itself, so this holds for float32/float64 NaN and is always false for
// integer types. Range rules must reject NaN because every comparison against it is
// false, which would otherwise let NaN silently pass.
func isNaN[T cmp.Ordered](v T) bool { return v != v }

// Min requires value >= min. NaN is rejected.
func Min[T cmp.Ordered](min T) Rule[T] {
	return func(v T) Violation {
		if isNaN(v) || v < min {
			return Violation{Key: "validation.min", Params: []Param{{Key: "min", Value: min}}}
		}
		return Violation{}
	}
}

// Max requires value <= max. NaN is rejected.
func Max[T cmp.Ordered](max T) Rule[T] {
	return func(v T) Violation {
		if isNaN(v) || v > max {
			return Violation{Key: "validation.max", Params: []Param{{Key: "max", Value: max}}}
		}
		return Violation{}
	}
}

// Between requires min <= value <= max. NaN is rejected.
func Between[T cmp.Ordered](min, max T) Rule[T] {
	return func(v T) Violation {
		if isNaN(v) || v < min || v > max {
			return Violation{Key: "validation.between", Params: []Param{{Key: "min", Value: min}, {Key: "max", Value: max}}}
		}
		return Violation{}
	}
}

// Positive requires value > 0.
func Positive[T number](v T) Violation {
	if v <= 0 {
		return Violation{Key: "validation.positive"}
	}
	return Violation{}
}

// Negative requires value < 0.
func Negative[T number](v T) Violation {
	if v >= 0 {
		return Violation{Key: "validation.negative"}
	}
	return Violation{}
}

// MultipleOf requires value to be an integer multiple of n.
func MultipleOf[T integer](n T) Rule[T] {
	return func(v T) Violation {
		if n == 0 || v%n != 0 {
			return Violation{Key: "validation.multiple_of", Params: []Param{{Key: "n", Value: n}}}
		}
		return Violation{}
	}
}
