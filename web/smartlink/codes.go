package smartlink

import (
	"strings"

	"github.com/dmitrymomot/forge/core/random"
)

// base58Alphabet is the Bitcoin-style base58 charset: no 0/O/I/l, which are
// easy to transpose in a printed or read-aloud short code.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// RandomCode returns a code generator producing n-character strings drawn
// from the base58 alphabet via [random.String]. Pass it to [WithCodeFunc] in
// place of the default (a lowercase Crockford [github.com/dmitrymomot/forge/core/id.Short])
// for shorter or differently-shaped generated codes.
func RandomCode(n int) func() string {
	return func() string { return random.String(n, base58Alphabet) }
}

// defaultReservedCodes are vanity codes that would shadow common app routes
// or well-known paths. [WithReservedCodes] extends this set.
var defaultReservedCodes = []string{
	"api", "admin", "app", "assets", "static", "health", "healthz", "metrics",
	"favicon.ico", "robots.txt", "login", "logout", "signup", "docs", "status",
	"www", ".well-known",
}

// newReservedSet returns a fresh map seeded with defaultReservedCodes, so
// each Manager owns an independent, separately extensible blocklist.
func newReservedSet() map[string]struct{} {
	set := make(map[string]struct{}, len(defaultReservedCodes))
	for _, c := range defaultReservedCodes {
		set[c] = struct{}{}
	}
	return set
}

// validCodeChars reports whether code is 1-64 characters, each one of
// [A-Za-z0-9_-] — the vanity code charset.
func validCodeChars(code string) bool {
	if code == "" || len(code) > 64 {
		return false
	}
	for i := range len(code) {
		c := code[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// macroElide strips {macro} placeholders from a target template, leaving
// only the literal text. Compile has already validated brace balance by the
// time this runs, so it is a plain scan, not a re-validation.
func macroElide(raw string) string {
	var b strings.Builder
	rest := raw
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			break
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			b.WriteString(rest)
			break
		}
		close += open
		b.WriteString(rest[:open])
		rest = rest[close+1:]
	}
	return b.String()
}

// authorityHasMacro reports whether raw's authority (host[:port]) segment —
// between "//" (or "scheme://") and the first '/', '?', or '#' — contains a
// macro placeholder, so a fully or partially dynamic host is recognized as
// intentional rather than a missing-host error.
func authorityHasMacro(raw string) bool {
	start := -1
	if i := strings.Index(raw, "://"); i >= 0 {
		start = i + 3
	} else if strings.HasPrefix(raw, "//") {
		start = 2
	}
	if start < 0 {
		return false
	}
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	return strings.ContainsRune(raw[start:end], '{')
}
