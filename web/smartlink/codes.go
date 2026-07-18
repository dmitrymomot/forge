package smartlink

import (
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
