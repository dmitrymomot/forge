package id

import "strings"

// Prefix is an immutable, concurrency-safe Stripe-style ID codec: a human prefix
// joined to a generated body by "_". The zero value is unusable; construct with
// NewPrefix. Options are optional; the default body generator is Short.
type Prefix struct {
	gen    func() string
	prefix string
	joined string // prefix + "_"
}

// PrefixOption configures NewPrefix.
type PrefixOption func(*Prefix)

// WithGenerator sets the body generator — the part after "<prefix>_". The default
// is Short (NewShort().String()); pass any func to mint ULID/UUID/random bodies,
// e.g. WithGenerator(func() string { return NewULID().String() }). A nil gen
// panics via NewPrefix.
func WithGenerator(gen func() string) PrefixOption {
	return func(p *Prefix) { p.gen = gen }
}

// NewPrefix returns a codec emitting IDs of the form "<prefix>_<body>". prefix
// must be non-empty and match [a-z0-9]+ (Stripe convention; "_" is the separator).
// Options are optional. It panics on an invalid prefix or a nil generator — both
// are boot-time programming errors.
func NewPrefix(prefix string, opts ...PrefixOption) Prefix {
	if !validPrefix(prefix) {
		panic("id: NewPrefix prefix must match [a-z0-9]+")
	}
	p := Prefix{
		prefix: prefix,
		joined: prefix + "_",
		gen:    func() string { return NewShort().String() },
	}
	for _, o := range opts {
		o(&p)
	}
	if p.gen == nil {
		panic("id: NewPrefix generator must not be nil")
	}
	return p
}

// New returns a fresh ID: "<prefix>_" + gen().
func (p Prefix) New() string { return p.joined + p.gen() }

// Parse validates that s carries p's prefix and a non-empty body, returning the
// body (the part after "<prefix>_"). Because the body generator is pluggable, the
// body is returned opaque — for the default Short generator, decode it with
// ParseShort. A wrong/absent prefix returns ErrWrongPrefix; an empty body returns
// ErrMalformed.
func (p Prefix) Parse(s string) (string, error) {
	body, ok := strings.CutPrefix(s, p.joined)
	if !ok {
		return "", ErrWrongPrefix
	}
	if body == "" {
		return "", ErrMalformed
	}
	return body, nil
}

// Is reports whether s carries p's prefix and a non-empty body. It validates the
// prefix and body presence only, not the body's internal format.
func (p Prefix) Is(s string) bool {
	_, err := p.Parse(s)
	return err == nil
}

// Prefix returns the bound prefix (for logging / diagnostics).
func (p Prefix) Prefix() string { return p.prefix }

// NewPrefixed returns a fresh "<prefix>_<short>" ID using the default Short
// generator. Equivalent to NewPrefix(prefix).New(); it panics on an invalid prefix.
func NewPrefixed(prefix string) string { return NewPrefix(prefix).New() }

// ParsePrefixed validates prefix on s and returns the body. Equivalent to
// NewPrefix(prefix).Parse(s).
func ParsePrefixed(prefix, s string) (string, error) { return NewPrefix(prefix).Parse(s) }

// IsPrefixed reports whether s carries prefix with a non-empty body. Equivalent to
// NewPrefix(prefix).Is(s).
func IsPrefixed(prefix, s string) bool { return NewPrefix(prefix).Is(s) }

// validPrefix reports whether s is a non-empty [a-z0-9]+ string.
func validPrefix(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
