package config

import (
	"fmt"
	"os"
	"strings"
)

// Substitute expands ${VAR} and ${VAR:default} placeholders in s against the
// process environment. $$ yields a literal $. ${VAR} with VAR unset is an
// error; ${VAR:default} falls back to default when VAR is unset OR empty. The
// name/default split is on the first colon, so defaults may contain colons.
func Substitute(s string) (string, error) {
	return substitute(s, os.LookupEnv)
}

func substitute(s string, lookup func(string) (string, bool)) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			rel := strings.IndexByte(s[i+2:], '}')
			if rel < 0 {
				return "", fmt.Errorf("%w: unterminated placeholder", ErrSubstitute)
			}
			val, err := resolvePlaceholder(s[i+2:i+2+rel], lookup)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i += 2 + rel + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}

func resolvePlaceholder(expr string, lookup func(string) (string, bool)) (string, error) {
	name, def, hasDefault := strings.Cut(expr, ":")
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: empty variable name", ErrSubstitute)
	}
	v, ok := lookup(name)
	if hasDefault {
		if !ok || v == "" {
			return def, nil
		}
		return v, nil
	}
	if !ok {
		return "", fmt.Errorf("%w: %s is not set", ErrSubstitute, name)
	}
	return v, nil
}
