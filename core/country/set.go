package country

import (
	"fmt"
	"sort"
	"strings"
)

// Set is an explicit, immutable collection of countries — a consumer's
// "supported countries" policy, shared by filtered UI dropdowns and phone's
// parse gate. The zero Set is a valid empty set: it contains nothing, so a
// gate configured with it fails closed.
type Set struct {
	m map[string]struct{} // uppercase alpha-2 keys
}

// NewSet builds a Set from Country values. Zero-value countries are ignored.
func NewSet(cs ...Country) Set {
	s := Set{m: make(map[string]struct{}, len(cs))}
	for _, c := range cs {
		if c.Alpha2 != "" {
			s.m[c.Alpha2] = struct{}{}
		}
	}
	return s
}

// NewSetFromCodes builds a Set from alpha-2 code strings (the form configuration
// supplies). It fails closed: an unknown code returns the zero Set wrapping
// ErrUnknownCode.
func NewSetFromCodes(codes ...string) (Set, error) {
	s := Set{m: make(map[string]struct{}, len(codes))}
	for _, code := range codes {
		c, ok := ByAlpha2(code)
		if !ok {
			return Set{}, fmt.Errorf("country: %q: %w", code, ErrUnknownCode)
		}
		s.m[c.Alpha2] = struct{}{}
	}
	return s, nil
}

// Contains reports whether c is in the set.
func (s Set) Contains(c Country) bool {
	_, ok := s.m[c.Alpha2]
	return ok
}

// ContainsCode reports whether the alpha-2 code is in the set, case-insensitively.
func (s Set) ContainsCode(code string) bool {
	_, ok := s.m[strings.ToUpper(code)]
	return ok
}

// All returns the set's countries sorted by Name — the filtered dropdown source.
func (s Set) All() []Country {
	out := make([]Country, 0, len(s.m))
	for code := range s.m {
		if c, ok := ByAlpha2(code); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Len returns the number of countries in the set.
func (s Set) Len() int {
	return len(s.m)
}
