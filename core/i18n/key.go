package i18n

import (
	"fmt"
	"strings"
)

// Key is a declared message key. Declare keys as package-level vars and pass
// them to Bundle.ValidateKeys at startup, so a typo is a construction error
// instead of a key echoed at a user in production:
//
//	var keyTitle = i18n.NewKey("dashboard.title")
//	if err := bundle.ValidateKeys(keyTitle); err != nil { ... }
//
// Key is a plain comparable value. TK and TNK delegate to T and TN — a Key
// costs exactly what a string key costs.
type Key struct {
	s string
}

// NewKey declares a message key.
func NewKey(s string) Key { return Key{s: s} }

// String returns the key text.
func (k Key) String() string { return k.s }

// TK is T with a declared Key.
func (b *Bundle) TK(loc Locale, k Key, args ...any) string {
	return b.T(loc, k.s, args...)
}

// TNK is TN with a declared Key.
func (b *Bundle) TNK(loc Locale, k Key, n int, args ...any) string {
	return b.TN(loc, k.s, n, args...)
}

// ValidateKeys reports keys absent from the default locale's catalog, naming
// every offender. It checks the default locale only: every lookup falls
// through to the default, so a key present there can never echo, and a key
// absent there always will.
//
// Validation is explicit rather than automatic inside New because a
// package-global key registry would break a binary constructing several
// bundles over different catalogs — each bundle must be checked against the
// keys that actually belong to it.
func (b *Bundle) ValidateKeys(keys ...Key) error {
	def := &b.locales[b.defaultIdx]
	var missing []string
	for _, k := range keys {
		if _, ok := def.messages[k.s]; ok {
			continue
		}
		if _, ok := def.plurals[k.s]; ok {
			continue
		}
		missing = append(missing, k.s)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s not in %s catalog", ErrUnknownKey, strings.Join(missing, ", "), def.tag)
}
