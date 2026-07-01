package validate

import "time"

// OneOf requires value to be one of the allowed values.
func OneOf[T comparable](allowed ...T) Rule[T] {
	return func(v T) Violation {
		for _, a := range allowed {
			if v == a {
				return Violation{}
			}
		}
		return Violation{Key: "validation.one_of", Params: []Param{{Key: "allowed", Value: allowed}}}
	}
}

// Equal requires value == other (e.g. confirm-password).
func Equal[T comparable](other T) Rule[T] {
	return func(v T) Violation {
		if v != other {
			return Violation{Key: "validation.equal"}
		}
		return Violation{}
	}
}

// NotEqual requires value != other.
func NotEqual[T comparable](other T) Rule[T] {
	return func(v T) Violation {
		if v == other {
			return Violation{Key: "validation.not_equal"}
		}
		return Violation{}
	}
}

// Before requires value to be strictly before u.
func Before(u time.Time) Rule[time.Time] {
	return func(v time.Time) Violation {
		if !v.Before(u) {
			return Violation{Key: "validation.before", Params: []Param{{Key: "other", Value: u}}}
		}
		return Violation{}
	}
}

// After requires value to be strictly after u.
func After(u time.Time) Rule[time.Time] {
	return func(v time.Time) Violation {
		if !v.After(u) {
			return Violation{Key: "validation.after", Params: []Param{{Key: "other", Value: u}}}
		}
		return Violation{}
	}
}
