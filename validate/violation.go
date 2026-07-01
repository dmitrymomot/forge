package validate

// Param is one interpolation value for a Violation's message. A []Param (ordered,
// ~1 alloc, and only on failure) is used instead of a map to keep the failure path cheap.
type Param struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Violation is one failed check — a value, never a heap pointer. The zero Violation
// (Key == "" && Message == "") means "passed": every rule returns it on success, so a
// passing check allocates nothing. Field is filled in by Apply/Manual.
type Violation struct {
	Field   string  `json:"field,omitempty"`
	Key     string  `json:"key,omitempty"`
	Params  []Param `json:"params,omitempty"`
	Message string  `json:"message,omitempty"`
}

// IsZero reports whether the check passed (no key and no message).
func (v Violation) IsZero() bool { return v.Key == "" && v.Message == "" }

// String implements fmt.Stringer: the Message when set, else the Key as a
// human-readable fallback (safe for logs/debug before any i18n rendering).
func (v Violation) String() string {
	if v.Message != "" {
		return v.Message
	}
	return v.Key
}

// Rule tests a value and returns a Violation (the zero Violation on success). It is
// the sanitize analog: func(T) Violation ↔ sanitize's func(T) T. Param-less rules are
// plain functions used bare (validate.Email); parameterized rules are constructors
// that return a Rule[T] (validate.MinLen(2)).
type Rule[T any] func(value T) Violation
