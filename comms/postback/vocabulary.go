package postback

import "fmt"

// Vocabulary is the set of macro names a platform exposes to its postback
// templates. It is consumer data — forge ships no names. The zero value is an
// empty vocabulary: every macro is unknown, so templates with placeholders
// fail closed at NewDestination.
type Vocabulary struct {
	names map[string]struct{}
}

// NewVocabulary registers the macro names templates may reference. Names are
// validated fail-closed: empty names or characters outside letters, digits,
// '_', '.', and '-' are ErrInvalidMacro. Duplicates collapse silently.
func NewVocabulary(names ...string) (Vocabulary, error) {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !validMacroName(name) {
			return Vocabulary{}, fmt.Errorf("%w: %q", ErrInvalidMacro, name)
		}
		set[name] = struct{}{}
	}
	return Vocabulary{names: set}, nil
}

func (v Vocabulary) contains(name string) bool {
	_, ok := v.names[name]
	return ok
}

func validMacroName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-' {
			continue
		}
		return false
	}
	return true
}
